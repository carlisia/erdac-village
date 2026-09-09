// The fast tier. No database and no network.
//
// What it can honestly assert is what stays in Go: the shape and the argument
// order of the statements this package builds, the translation of a driver
// error into a domain one, and the checks that refuse a write before the
// server ever sees it. Everything the database itself enforces is asserted by
// the live tier, because a mock driver accepts whatever it is told to accept
// and would only be agreeing with this file's beliefs about MySQL.
//
// Statements are matched on a distinctive fragment rather than in full.
// Matching a whole statement breaks on every harmless rewrite, and a test that
// fails when nothing is wrong stops being read.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/carlisia/erdac-village/internal/config"
	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// newMock returns a store backed by a mock driver, and fails the test at the
// end if any expectation went unmet. Expectations are ordered, which is what
// lets the publish tests assert an ordering rather than a set.
func newMock(t *testing.T) (*DB, sqlmock.Sqlmock) {
	t.Helper()
	raw, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("open the mock driver: %v", err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
		raw.Close()
	})
	return New(sqlx.NewDb(raw, "mysql")), mock
}

// frag matches a statement by a distinctive fragment of it.
func frag(s string) string { return regexp.QuoteMeta(s) }

// lockReadStatement is the read that takes the lock, its start time and the
// server's clock together. It is named because eleven tests stub it, and the
// three columns have to be listed in the order the statement selects them.
const lockReadStatement = "SELECT fetch_lock, fetch_started_at, UTC_TIMESTAMP(6) FROM system_state"

// expectLockRead stubs that read.
//
// owner is the run holding the lock, or nil for no lock at all. startedAt is
// when it was taken, or nil for a lock with no start time, which should not
// occur and is deliberately read as live. The server's clock is always the
// test's own instant, so a lock's age is set by how far back startedAt is.
//
// The column list lives here rather than at each call site. Written out
// eleven times it was eleven chances to get the order wrong, and a wrong order
// stubs a lock that reads as something else entirely.
func expectLockRead(mock sqlmock.Sqlmock, owner, startedAt any) {
	mock.ExpectQuery(frag(lockReadStatement)).
		WillReturnRows(sqlmock.NewRows([]string{"fetch_lock", "fetch_started_at", "now"}).
			AddRow(owner, startedAt, testTime))
}

var testTime = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

// expectPublishUpTo sets the expectations every publish shares, up to and
// including the retirement set. The tests that follow differ only in what they
// expect after that point.
func expectPublishUpTo(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	expectLockRead(mock, nil, nil)
	mock.ExpectQuery(frag("FROM documents d")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "url"}).AddRow(int64(7), "/about"))
	mock.ExpectQuery(frag("SELECT id FROM documents WHERE state = 'published'")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(3)))
}

// The order inside publish is the specification. Promoting before retiring
// collides with the live page for the same address, which is a bug the
// original system shipped once, so the order is asserted rather than left to
// whoever next edits the method.
func TestPublishRetiresBeforeItPromotes(t *testing.T) {
	db, mock := newMock(t)
	expectPublishUpTo(mock)
	mock.ExpectExec(frag("SET state = 'superseded'")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(frag("SET state = 'published'")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(frag("DELETE FROM documents WHERE id IN")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(frag("UPDATE system_state SET last_published_at")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := db.Publish(context.Background(), testTime)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got.Promoted != 1 || got.Retired != 1 {
		t.Errorf("publish reported %+v, want one promoted and one retired", got)
	}
}

// The previous test only means something if the assertion could fail. This is
// the same run with the two statements expected the other way round: it must
// be refused, which is what proves the ordering is being checked rather than
// merely described.
func TestPublishOrderingIsActuallyChecked(t *testing.T) {
	raw, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	db := New(sqlx.NewDb(raw, "mysql"))

	expectPublishUpTo(mock)
	mock.ExpectExec(frag("SET state = 'published'")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(frag("SET state = 'superseded'")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	if _, err := db.Publish(context.Background(), testTime); err == nil {
		t.Fatal("a publish that promoted before retiring was accepted by the test harness, " +
			"so the ordering assertion proves nothing")
	}
}

// A publish that fails part-way must leave the corpus exactly as it was. The
// rollback is what makes that true, so its absence is the failure to catch.
func TestPublishRollsBackWhenPromotionFails(t *testing.T) {
	db, mock := newMock(t)
	expectPublishUpTo(mock)
	mock.ExpectExec(frag("SET state = 'superseded'")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(frag("SET state = 'published'")).WillReturnError(errors.New("connection lost"))
	mock.ExpectRollback()

	if _, err := db.Publish(context.Background(), testTime); err == nil {
		t.Fatal("a failed promotion was reported as a successful publish")
	}
}

// Promoting a half-written candidate set is what the fetch lock exists to
// prevent, so publish must refuse while one is held and must do no work at
// all: the expectations below end at the rollback.
func TestPublishIsRefusedWhileAFetchRuns(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	expectLockRead(mock, "run-1", testTime.Add(-time.Minute))
	mock.ExpectRollback()

	_, err := db.Publish(context.Background(), testTime)
	if !errors.Is(err, ErrPublishDuringFetch) {
		t.Fatalf("publish during a fetch returned %v, want ErrPublishDuringFetch", err)
	}
}

// The lock's age is measured against the server's clock, never against the
// timestamp the caller wants the publish recorded under. Here the server says
// the lock was taken a minute ago while the caller stamps the publish a year
// hence: the fetch is running, and the publish must be refused.
func TestPublishJudgesTheLockOnTheServersClock(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	expectLockRead(mock, "run-1", testTime.Add(-time.Minute))
	mock.ExpectRollback()

	_, err := db.Publish(context.Background(), testTime.AddDate(1, 0, 0))
	if !errors.Is(err, ErrPublishDuringFetch) {
		t.Fatalf("a publish stamped a year ahead got %v, want ErrPublishDuringFetch", err)
	}
}

// A crashed run leaves its lock behind and its candidate set half written.
// Expiring the lock lets a new fetch start; it must not let a publish promote
// what the dead run wrote, because that is half of one crawl.
//
// An earlier version of this test asserted the opposite, and the code obeyed
// it. Expiring a lock and trusting the pages under it are two decisions, and
// running them together made the second one invisible.
func TestPublishIsRefusedAfterAnAbandonedFetch(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	expectLockRead(mock, "crashed", testTime.Add(-FetchLockTTL-time.Minute))
	mock.ExpectRollback()

	_, err := db.Publish(context.Background(), testTime)
	if !errors.Is(err, ErrAbandonedFetch) {
		t.Fatalf("got %v, want ErrAbandonedFetch", err)
	}
	if errors.Is(err, ErrPublishDuringFetch) {
		t.Error("an abandoned fetch was reported as a running one, which has a different remedy")
	}
	if !strings.Contains(err.Error(), "crashed") {
		t.Errorf("the message does not name the run that left the lock: %q", err)
	}
}

// The other half of the same rule: a crashed run must not lock the system out,
// so a new fetch still takes the expired lock over.
func TestAnAbandonedLockStillLetsANewFetchStart(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	expectLockRead(mock, "crashed", testTime.Add(-FetchLockTTL-time.Minute))
	mock.ExpectExec(frag("SET fetch_lock = ?, fetch_started_at = UTC_TIMESTAMP(6)")).
		WithArgs("run-2").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := db.AcquireFetchLock(context.Background(), "run-2"); err != nil {
		t.Fatalf("an abandoned lock blocked a new fetch: %v", err)
	}
}

// A publish with nothing to promote still records that it ran. That is what
// tells an administrator that nothing changed rather than that nothing was
// reviewed.
func TestPublishWithNoCandidatesStillRecordsTheTime(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	expectLockRead(mock, nil, nil)
	mock.ExpectQuery(frag("FROM documents d")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "url"}))
	mock.ExpectExec(frag("UPDATE system_state SET last_published_at")).
		WithArgs(testTime).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := db.Publish(context.Background(), testTime)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got.Promoted != 0 {
		t.Errorf("publish reported %d promoted with no candidates", got.Promoted)
	}
}

// The two uniqueness rules must reach a caller as two different errors. They
// are told apart by the index named in the server's message, so this asserts
// the translation rather than the rule, which only a real server can enforce.
func TestDuplicateKeysBecomeDistinctDomainErrors(t *testing.T) {
	for _, c := range []struct {
		index string
		want  error
	}{
		{"documents_one_candidate_per_url", ErrDuplicateCandidate},
		{"documents_one_published_per_url", ErrDuplicatePublished},
	} {
		t.Run(c.index, func(t *testing.T) {
			db, mock := newMock(t)
			mock.ExpectBegin()
			mock.ExpectQuery(frag("SELECT id FROM documents WHERE url_candidate")).
				WillReturnError(sql.ErrNoRows)
			mock.ExpectExec(frag("INSERT INTO documents")).WillReturnError(&mysql.MySQLError{
				Number:  errDuplicateEntry,
				Message: "Duplicate entry '/about' for key '" + c.index + "'",
			})
			mock.ExpectRollback()
			_, err := db.UpsertCandidate(context.Background(), Page{URL: "/about", FetchedAt: testTime})
			if !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

// A duplicate on some other index is not one of the review rules and must not
// be dressed up as one, because a caller matching the sentinel would then
// retry something that will never succeed.
func TestAnUnrelatedDuplicateIsNotTranslated(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(frag("DELETE FROM chunks")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(frag("INSERT INTO chunks")).WillReturnError(&mysql.MySQLError{
		Number:  errDuplicateEntry,
		Message: "Duplicate entry '1-0' for key 'chunks_one_per_ordinal'",
	})
	mock.ExpectRollback()

	_, err := db.ReplaceChunks(context.Background(), 1, []Chunk{{Ordinal: 0, Text: "t"}})
	if err == nil {
		t.Fatal("the duplicate was not reported at all")
	}
	if errors.Is(err, ErrDuplicateCandidate) || errors.Is(err, ErrDuplicatePublished) {
		t.Fatalf("an unrelated duplicate was reported as a review-rule violation: %v", err)
	}
}

// A deadlock the database broke, and a row lock this caller waited too long
// for, are the two failures where retrying is the correct response and every
// other error here means the opposite. A caller that cannot tell them apart
// either retries nothing or retries everything.
func TestTransientDatabaseFailuresAreTheirOwnClass(t *testing.T) {
	for _, c := range []struct {
		name   string
		number uint16
	}{
		{"deadlock", errLockDeadlock},
		{"lock wait timeout", errLockWaitTimeout},
	} {
		t.Run(c.name, func(t *testing.T) {
			db, mock := newMock(t)
			mock.ExpectBegin()
			mock.ExpectQuery(frag("SELECT id FROM documents WHERE url_candidate")).
				WillReturnError(&mysql.MySQLError{Number: c.number, Message: c.name})
			mock.ExpectRollback()

			_, err := db.UpsertCandidate(context.Background(), Page{URL: "/x", FetchedAt: testTime})
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("got %v, want ErrConflict", err)
			}
			for _, other := range []error{ErrDuplicateCandidate, ErrDuplicatePublished, ErrAddressTooLong} {
				if errors.Is(err, other) {
					t.Errorf("a transient failure also matched %v, so a caller cannot tell them apart", other)
				}
			}
		})
	}
}

// The seed row is written by the migration, so its absence means the database
// was not brought up properly. Every method that reads it must say that rather
// than pass a driver error up, and each must name itself, because the same
// sentence from six operations tells an administrator nothing.
//
// The writers are here too. An update whose WHERE matches nothing is not an
// error to the driver, so a writer that does not go on to look for the row
// reports success against a database that was never seeded. Publish has its
// own test below because its expectations do not fit this table.
func TestAMissingSystemStateRowIsNamedByEveryReader(t *testing.T) {
	noRow := func(m sqlmock.Sqlmock, query string) {
		m.ExpectQuery(frag(query)).WillReturnError(sql.ErrNoRows)
	}
	for _, c := range []struct {
		name   string
		expect func(sqlmock.Sqlmock)
		call   func(*DB) error
	}{
		{"read the system state", func(m sqlmock.Sqlmock) {
			noRow(m, "SELECT halted")
		}, func(d *DB) error {
			_, err := d.SystemState(context.Background())
			return err
		}},
		{"release the fetch lock", func(m sqlmock.Sqlmock) {
			m.ExpectBegin()
			noRow(m, "SELECT fetch_lock FROM system_state")
			m.ExpectRollback()
		}, func(d *DB) error {
			return d.ReleaseFetchLock(context.Background(), "run", nil)
		}},
		{"take the fetch lock", func(m sqlmock.Sqlmock) {
			m.ExpectBegin()
			noRow(m, "SELECT fetch_lock, fetch_started_at, UTC_TIMESTAMP(6) FROM system_state")
			m.ExpectRollback()
		}, func(d *DB) error {
			return d.AcquireFetchLock(context.Background(), "run")
		}},
		{"clear conversations", func(m sqlmock.Sqlmock) {
			m.ExpectBegin()
			m.ExpectExec(frag("session_epoch = session_epoch + 1")).WillReturnResult(sqlmock.NewResult(0, 0))
			noRow(m, "SELECT session_epoch FROM system_state")
			m.ExpectRollback()
		}, func(d *DB) error {
			_, err := d.ClearConversations(context.Background())
			return err
		}},
		{"set the halted flag", func(m sqlmock.Sqlmock) {
			m.ExpectExec(frag("UPDATE system_state SET halted")).WillReturnResult(sqlmock.NewResult(0, 0))
			noRow(m, "SELECT 1 FROM system_state")
		}, func(d *DB) error {
			return d.SetHalted(context.Background(), true)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			db, mock := newMock(t)
			c.expect(mock)
			err := c.call(db)
			if !errors.Is(err, ErrNoSystemState) {
				t.Fatalf("got %v, want ErrNoSystemState", err)
			}
			if !strings.Contains(err.Error(), c.name) {
				t.Errorf("the message does not name the operation that hit it: %q", err)
			}
		})
	}
}

// The reason the halt write looks for the row rather than reading its own
// row count: the driver reports rows changed, not rows matched, so setting the
// flag to the value it already holds changes nothing and would otherwise be
// indistinguishable from a row that is not there. Here the update changes
// nothing and the row exists, and that has to be a success.
func TestHaltingToTheValueAlreadyHeldIsNotAMissingRow(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectExec(frag("UPDATE system_state SET halted")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(frag("SELECT 1 FROM system_state")).
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))

	if err := db.SetHalted(context.Background(), true); err != nil {
		t.Fatalf("halting to the value already held was refused: %v", err)
	}
}

// A failure that arrives while the rows are streaming is reported by the
// iteration rather than by the query, and it is still a driver error. A
// deadlock broken mid-read has to reach the caller as the retryable class with
// the operation named, exactly as one broken before the first row does.
// Returning the iteration error bare would do neither.
func TestAFailureMidIterationIsTranslatedAndNamed(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectQuery(frag("FROM address_settings")).
		WillReturnRows(sqlmock.NewRows([]string{
			"url", "included",
			"id", "title", "content_hash", "fetched_at", "published_at",
			"id", "title", "content_hash", "fetched_at", "published_at",
		}).AddRow("/one", true, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).
			RowError(0, &mysql.MySQLError{Number: errLockDeadlock, Message: "Deadlock found"}))

	_, err := db.AddressStates(context.Background())
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("a deadlock during iteration got %v, want ErrConflict", err)
	}
	if !strings.Contains(err.Error(), "read address states") {
		t.Errorf("the message does not name the operation that hit it: %q", err)
	}
}

// A lock with no start time is treated as held, and the refusal must not print
// the zero time: a date in the year one reads as a clock fault and sends the
// reader to the wrong place.
func TestARefusalForALockWithNoStartTimeDoesNotPrintTheZeroTime(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	expectLockRead(mock, "run-1", nil)
	mock.ExpectRollback()

	err := db.AcquireFetchLock(context.Background(), "run-2")
	if !errors.Is(err, ErrFetchInProgress) {
		t.Fatalf("got %v, want ErrFetchInProgress", err)
	}
	if strings.Contains(err.Error(), "0001-01-01") {
		t.Errorf("the refusal prints the zero time: %q", err)
	}
	if !strings.Contains(err.Error(), "run-1") {
		t.Errorf("the refusal does not name the holder: %q", err)
	}
}

// Releasing a lock nobody holds is refused like releasing one another run took,
// and the message says which, because the remedies differ: the first is a
// double release in the caller, the second an overrun. An empty name in quotes
// would read as the second with the name missing.
func TestReleasingALockNobodyHoldsSaysSo(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(frag("SELECT fetch_lock FROM system_state")).
		WillReturnRows(sqlmock.NewRows([]string{"fetch_lock"}).AddRow(nil))
	mock.ExpectRollback()

	err := db.ReleaseFetchLock(context.Background(), "run-1", nil)
	if !errors.Is(err, ErrFetchLockLost) {
		t.Fatalf("got %v, want ErrFetchLockLost", err)
	}
	if strings.Contains(err.Error(), `""`) {
		t.Errorf("the refusal prints an empty holder: %q", err)
	}
	if !strings.Contains(err.Error(), "nobody") {
		t.Errorf("the refusal does not say the lock is unheld: %q", err)
	}
}

// A publish that cannot find the seeded row must say so rather than report a
// driver error, because the two have completely different remedies.
func TestPublishNamesAMissingSystemStateRow(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(frag(lockReadStatement)).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err := db.Publish(context.Background(), testTime)
	if !errors.Is(err, ErrNoSystemState) {
		t.Fatalf("got %v, want ErrNoSystemState", err)
	}
	if !strings.Contains(err.Error(), "publish") {
		t.Errorf("the message does not name the operation that hit it: %q", err)
	}
}

// A server without the vector extension does not know the function, and only
// that says the extension is absent. Every other failure reported as an absent
// extension sends an operator to install something already installed.
func TestOnlyAnUnknownFunctionMeansTheExtensionIsMissing(t *testing.T) {
	t.Run("unknown function", func(t *testing.T) {
		db, mock := newMock(t)
		mock.ExpectQuery(frag("SELECT VECTOR_MAX_DIMENSION()")).WillReturnError(&mysql.MySQLError{
			Number:  errFunctionNotFound,
			Message: "FUNCTION db.VECTOR_MAX_DIMENSION does not exist",
		})
		if err := db.checkVectorExtension(context.Background()); !errors.Is(err, ErrVectorExtensionMissing) {
			t.Fatalf("got %v, want ErrVectorExtensionMissing", err)
		}
	})
	t.Run("a dropped connection is not a missing extension", func(t *testing.T) {
		db, mock := newMock(t)
		mock.ExpectQuery(frag("SELECT VECTOR_MAX_DIMENSION()")).
			WillReturnError(errors.New("invalid connection"))
		err := db.checkVectorExtension(context.Background())
		if err == nil {
			t.Fatal("a dropped connection was not reported at all")
		}
		if errors.Is(err, ErrVectorExtensionMissing) {
			t.Fatalf("a dropped connection was reported as a missing extension: %v", err)
		}
	})
}

// The vector column is declared at one width and the embedding function offers
// no way to ask for fewer elements, so a server that cannot hold that width
// cannot run this system at all. This is the single constraint the whole
// design rests on.
func TestAServerTooNarrowForTheEmbeddingIsRefused(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectQuery(frag("SELECT VECTOR_MAX_DIMENSION()")).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(config.MaxVectorDimensions - 1))

	err := db.checkVectorExtension(context.Background())
	if !errors.Is(err, ErrVectorTooNarrow) {
		t.Fatalf("got %v, want ErrVectorTooNarrow", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprint(config.MaxVectorDimensions)) {
		t.Errorf("the message does not say how wide an embedding is: %q", err)
	}
}

// The live tier is selected by a name pattern, which nothing but this test
// enforces. A live test renamed out of the pattern still compiles, still skips
// without a connection string, and is silently never run with one.
//
// It walks the module rather than reading a list of files. A list is the same
// failure one level up: a live-tier file added somewhere the list does not name
// goes unguarded, and this test reports success having checked nothing, which
// is precisely what it exists to prevent.
func TestEveryLiveTestIsNamedSoTheCheckScriptFindsIt(t *testing.T) {
	root := moduleRoot(t)
	named := regexp.MustCompile(`(?m)^func (Test\w+)`)

	var files int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "live_test.go" && !strings.HasSuffix(d.Name(), "_live_test.go") {
			return nil
		}
		files++
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, m := range named.FindAllStringSubmatch(string(src), -1) {
			if !strings.HasPrefix(m[1], "TestLive") {
				t.Errorf("%s: %s does not begin with TestLive, so the check script's pattern skips it",
					rel, m[1])
			}
			// Each live test creates a scratch database named after itself, and
			// MySQL caps a database name at 64 characters. The live tier refuses
			// to truncate, so an overlong name fails there, which is the tier
			// that runs least. Three names were over the cap for a day before
			// anyone ran it. The longest prefix any tier uses is the budget.
			if n := len(scratchPrefixLongest) + len(scratchSafe(m[1])); n > 64 {
				t.Errorf("%s: %s would make a %d-character scratch database name; MySQL allows 64",
					rel, m[1], n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}
	if files == 0 {
		t.Fatal("no live-tier files were found, so this guard checked nothing")
	}
}

// scratchPrefixLongest is the longer of the two prefixes the live tiers put on
// a scratch database name. Using it for every file is one character stricter
// than necessary for the store's tier, which is a fair price for one rule.
const scratchPrefixLongest = "village_schema_"

// scratchSafe mirrors what the live tiers do to a test name: lower it and keep
// only what a database name may contain.
func scratchSafe(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// moduleRoot walks up from the test's own directory to the directory holding
// go.mod, so the guard does not depend on how deep the package that runs it
// happens to sit.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's directory, so the module root cannot be found")
		}
		dir = parent
	}
}

// The specification is the reference for what this system does, and it states
// how many failures a caller can tell apart. A sentinel added without an entry
// there leaves the document describing an earlier version of the code.
//
// Counting is the point rather than a shortcut. The count was wrong once, by
// four, because it was written by hand from a list that had grown since. A
// number nothing checks is a number that drifts.
func TestTheSpecificationCountsTheSentinelsThisPackageDeclares(t *testing.T) {
	spec := filepath.Join(moduleRoot(t), "docs", "specs", "0001-schema-and-review-workflow.md")
	text, err := os.ReadFile(spec)
	if err != nil {
		t.Fatalf("read the specification: %v", err)
	}
	stated := regexp.MustCompile(`\*\*(\w+) sentinel errors`).FindSubmatch(text)
	if stated == nil {
		t.Fatal("the specification no longer states how many sentinel errors there are")
	}
	words := map[string]int{
		"Four": 4, "Five": 5, "Six": 6, "Seven": 7, "Eight": 8, "Nine": 9, "Ten": 10,
		"Eleven": 11, "Twelve": 12, "Thirteen": 13, "Fourteen": 14, "Fifteen": 15,
		"Sixteen": 16, "Seventeen": 17, "Eighteen": 18, "Nineteen": 19, "Twenty": 20,
	}
	want, ok := words[string(stated[1])]
	if !ok {
		t.Fatalf("the specification says %q sentinel errors, which is not a number this test knows; "+
			"add it to the list here", stated[1])
	}

	src, err := os.ReadFile("errors.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := regexp.MustCompile(`(?m)^\tErr\w+ = errors\.New`).FindAll(src, -1)
	if len(declared) != want {
		t.Errorf("the specification says %d sentinel errors and this package declares %d; "+
			"add the new one to the specification's list of additions, or correct the count",
			want, len(declared))
	}
}

// An address the column cannot hold is a failed page, never a truncated one.
// This is refused before the server sees it, so the behaviour is the same on a
// server that is not running in strict mode.
func TestAnOverlongAddressIsRefusedWithoutAStatement(t *testing.T) {
	db, _ := newMock(t)
	long := "/" + strings.Repeat("a", MaxAddressLength)

	_, err := db.UpsertCandidate(context.Background(), Page{URL: long, FetchedAt: testTime})
	if !errors.Is(err, ErrAddressTooLong) {
		t.Fatalf("upsert returned %v, want ErrAddressTooLong", err)
	}
	if err := db.SetAddressIncluded(context.Background(), long, false, testTime); !errors.Is(err, ErrAddressTooLong) {
		t.Fatalf("judgement returned %v, want ErrAddressTooLong", err)
	}
	// No expectations were registered, so the cleanup assertion proves no
	// statement was sent.
}

// The limit is in characters because the column is declared in characters. An
// address that fits in the column but not in 768 bytes must be accepted.
func TestAMultiByteAddressIsMeasuredInCharacters(t *testing.T) {
	db, mock := newMock(t)
	// The accented letter here is the point of the test, not a slip past the
	// repository's ASCII rule: it is two bytes and one character, which is the
	// difference being asserted.
	url := "/" + strings.Repeat("é", MaxAddressLength-1)
	mock.ExpectBegin()
	mock.ExpectQuery(frag("SELECT id FROM documents WHERE url_candidate")).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(frag("INSERT INTO documents")).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if _, err := db.UpsertCandidate(context.Background(), Page{URL: url, FetchedAt: testTime}); err != nil {
		t.Fatalf("an address of %d characters was refused: %v", len([]rune(url)), err)
	}
}

// The server's own refusal must reach a caller as the same error as the local
// check, so a caller has one thing to match rather than two.
func TestTheServersTruncationRefusalIsTranslated(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(frag("SELECT id FROM documents WHERE url_candidate")).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(frag("INSERT INTO documents")).WillReturnError(&mysql.MySQLError{
		Number:  errDataTooLong,
		Message: "Data too long for column 'url' at row 1",
	})
	mock.ExpectRollback()

	_, err := db.UpsertCandidate(context.Background(), Page{URL: "/x", FetchedAt: testTime})
	if !errors.Is(err, ErrAddressTooLong) {
		t.Fatalf("got %v, want ErrAddressTooLong", err)
	}
}

// The server reports every column that is too wide with the same error number,
// and only the address means what ErrAddressTooLong says. An overlong title
// reported as an overlong address sends a caller to inspect the wrong field.
func TestAnOverlongColumnThatIsNotTheAddressIsNotTranslated(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(frag("SELECT id FROM documents WHERE url_candidate")).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(frag("INSERT INTO documents")).WillReturnError(&mysql.MySQLError{
		Number:  errDataTooLong,
		Message: "Data too long for column 'title' at row 1",
	})
	mock.ExpectRollback()

	_, err := db.UpsertCandidate(context.Background(), Page{URL: "/x", FetchedAt: testTime})
	if err == nil {
		t.Fatal("an overlong title was not reported at all")
	}
	if errors.Is(err, ErrAddressTooLong) {
		t.Fatalf("an overlong title was reported as an overlong address: %v", err)
	}
}

// Re-fetching a page whose content has not changed must still report which
// page it is, because the caller writes that page's chunks next. The
// on-duplicate form of this statement could not: MySQL skips the update when
// every assigned value is already what it was, and the identifier is recovered
// by an assignment inside that update.
func TestReFetchingAnUnchangedPageStillReportsItsIdentifier(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(frag("SELECT id FROM documents WHERE url_candidate")).
		WithArgs("/same").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectExec(frag("UPDATE documents")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	id, err := db.UpsertCandidate(context.Background(), Page{URL: "/same", FetchedAt: testTime})
	if err != nil {
		t.Fatalf("re-fetch: %v", err)
	}
	if id != 42 {
		t.Errorf("re-fetching an unchanged page reported page %d, want 42", id)
	}
}

// A vector of the wrong width cannot be stored, and the whole batch must be
// refused before any of it is written. Writing the good ones first would leave
// a page half embedded and report a failure, which is the state that is
// hardest to recover from.
func TestAWrongWidthEmbeddingRefusesTheWholeBatch(t *testing.T) {
	db, _ := newMock(t)
	good := make([]float32, config.MaxVectorDimensions)
	err := db.StoreEmbeddings(context.Background(), []Embedding{
		{ChunkID: 1, Vector: good},
		{ChunkID: 2, Vector: good[:10]},
	})
	if !errors.Is(err, ErrWrongVectorWidth) {
		t.Fatalf("got %v, want ErrWrongVectorWidth", err)
	}
	// No expectations registered: nothing was written.
}

// A page must never end up half embedded: half its text would answer
// questions and the run would report a failure, which is the state that is
// hardest to tell apart from a healthy one.
//
// What this can prove is that the write opens a transaction and rolls it back.
// It cannot prove the inserts are inside that transaction, because the mock
// driver hands every statement the one connection and so sees the same
// sequence either way. TestLiveAFailedEmbeddingLeavesNoneBehind asserts the
// atomicity itself, against a server that can actually refuse one.
func TestAFailedEmbeddingRollsBackTheOnesBeforeIt(t *testing.T) {
	db, mock := newMock(t)
	v := make([]float32, config.MaxVectorDimensions)
	mock.ExpectBegin()
	mock.ExpectExec(frag("INSERT INTO embeddings")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(frag("INSERT INTO embeddings")).WillReturnError(errors.New("connection lost"))
	mock.ExpectRollback()

	err := db.StoreEmbeddings(context.Background(), []Embedding{
		{ChunkID: 1, Vector: v}, {ChunkID: 2, Vector: v},
	})
	if err == nil {
		t.Fatal("a failed embedding run was reported as a success")
	}
}

// The vector reaches the column as a literal because nothing casts a computed
// vector into the fixed-width column type. This asserts the literal is built
// as the column expects and that no caller-supplied text can reach it.
func TestTheVectorIsWrittenAsALiteral(t *testing.T) {
	if got := formatVector([]float32{0.5, -1, 0}); got != "[0.5,-1,0]" {
		t.Errorf("formatVector = %q, want [0.5,-1,0]", got)
	}
	v := make([]float32, config.MaxVectorDimensions)
	got := formatVector(v)
	if strings.Count(got, ",") != config.MaxVectorDimensions-1 {
		t.Errorf("a full-width vector rendered %d separators, want %d",
			strings.Count(got, ","), config.MaxVectorDimensions-1)
	}
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Error("the literal is not bracketed, so the column would refuse it")
	}
	if strings.ContainsAny(got, "'\"\\") {
		t.Error("the literal contains a quote, so concatenating it would not be safe")
	}
}

// A normalised embedding is full of negative elements and of magnitudes small
// enough that the shortest representation of a float switches to exponent
// notation. Whether the column's parser accepts an exponent has never been
// asked of the server, so the literal must not contain one.
func TestTheVectorLiteralNeverUsesExponentNotation(t *testing.T) {
	realistic := []float32{0.1, -0.1, 0.0123, -0.00456, 1.2e-05, -3.4e-06, 0, 1e-08}
	got := formatVector(realistic)
	if strings.ContainsAny(got, "eE") {
		t.Fatalf("the literal uses exponent notation, which the column may not parse: %s", got)
	}
	for _, want := range []string{"-0.1", "0.000012", "-0.0000034"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing from the literal: %s", want, got)
		}
	}
}

// A second fetch must not be able to start, because two crawls interleaving
// produce one candidate set that looks healthy and mixes two runs.
func TestASecondFetchIsRefused(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	expectLockRead(mock, "run-1", testTime.Add(-time.Minute))
	mock.ExpectRollback()

	err := db.AcquireFetchLock(context.Background(), "run-2")
	if !errors.Is(err, ErrFetchInProgress) {
		t.Fatalf("got %v, want ErrFetchInProgress", err)
	}
}

// A crashed run must not lock the system out permanently.
func TestAnAbandonedLockIsTakenOver(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	expectLockRead(mock, "crashed", testTime.Add(-FetchLockTTL-time.Second))
	mock.ExpectExec(frag("SET fetch_lock = ?, fetch_started_at = UTC_TIMESTAMP(6)")).
		WithArgs("run-2").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := db.AcquireFetchLock(context.Background(), "run-2"); err != nil {
		t.Fatalf("an abandoned lock was not taken over: %v", err)
	}
}

// A run that overran its lock must not clear the lock a live run now holds.
func TestReleasingALockAnotherRunTookIsRefused(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(frag("SELECT fetch_lock FROM system_state")).
		WillReturnRows(sqlmock.NewRows([]string{"fetch_lock"}).AddRow("run-2"))
	mock.ExpectRollback()

	if err := db.ReleaseFetchLock(context.Background(), "run-1", &testTime); !errors.Is(err, ErrFetchLockLost) {
		t.Fatalf("got %v, want ErrFetchLockLost", err)
	}
}

// A failed run must not record a fetch time, because a recorded time is what
// an administrator reads as "the site was checked".
func TestAFailedRunReleasesWithoutRecordingAFetch(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(frag("SELECT fetch_lock FROM system_state")).
		WillReturnRows(sqlmock.NewRows([]string{"fetch_lock"}).AddRow("run-1"))
	mock.ExpectExec(frag("SET fetch_lock = NULL, fetch_started_at = NULL WHERE")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := db.ReleaseFetchLock(context.Background(), "run-1", nil); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// The expiry rule, at its edges, and the reading of a lock with no start time.
func TestFetchLockExpiry(t *testing.T) {
	now := testTime
	lockOf := func(owner string, age time.Duration, hasStart bool) fetchLock {
		return fetchLock{
			owner:     sql.NullString{String: owner, Valid: owner != ""},
			startedAt: sql.NullTime{Time: now.Add(-age), Valid: hasStart},
			serverNow: now,
		}
	}
	for _, c := range []struct {
		name      string
		lock      string
		age       time.Duration
		hasStart  bool
		held      bool
		abandoned bool
	}{
		{"no lock", "", 0, false, false, false},
		{"just taken", "run", 0, true, true, false},
		{"one second short of expiry", "run", FetchLockTTL - time.Second, true, true, false},
		{"exactly at expiry", "run", FetchLockTTL, true, false, true},
		{"long expired", "run", 10 * FetchLockTTL, true, false, true},
		{"held with no start time is treated as live", "run", 0, false, true, false},
	} {
		f := lockOf(c.lock, c.age, c.hasStart)
		if got := f.held(); got != c.held {
			t.Errorf("%s: held = %v, want %v", c.name, got, c.held)
		}
		if got := f.abandoned(); got != c.abandoned {
			t.Errorf("%s: abandoned = %v, want %v", c.name, got, c.abandoned)
		}
		if f.held() && f.abandoned() {
			t.Errorf("%s: a lock cannot be both live and abandoned", c.name)
		}
	}
}

// An address with no recorded judgement is included, which is what makes the
// common case cost nothing: only an exclusion is ever written down.
//
// The two pages are asserted by value rather than only by presence, so that
// reading them from each other's columns fails here. Presence alone would pass
// with the two swapped, and swapped is the shape this query is most likely to
// get wrong: it joins the same table twice.
func TestAnAddressWithNoJudgementIsIncluded(t *testing.T) {
	db, mock := newMock(t)
	live := testTime.Add(-time.Hour)
	mock.ExpectQuery(frag("FROM address_settings")).
		WillReturnRows(sqlmock.NewRows([]string{
			"url", "included",
			"id", "title", "content_hash", "fetched_at", "published_at",
			"id", "title", "content_hash", "fetched_at", "published_at",
		}).AddRow("/one", true,
			int64(3), "Live", "live-hash", live, live,
			int64(7), "Pending", "pending-hash", testTime, nil))

	got, err := db.AddressStates(context.Background())
	if err != nil {
		t.Fatalf("address states: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d addresses, want 1", len(got))
	}
	a := got[0]
	if !a.Included {
		t.Error("an address with no recorded judgement was reported excluded")
	}
	if a.Published == nil || a.Candidate == nil {
		t.Fatalf("an address holding both a live page and a pending one reported %+v", a)
	}
	if a.Published.ID != 3 || a.Published.ContentHash != "live-hash" || a.Published.Title != "Live" {
		t.Errorf("the live page read back as %+v, want the columns the query selects for it", a.Published)
	}
	if a.Candidate.ID != 7 || a.Candidate.ContentHash != "pending-hash" || a.Candidate.Title != "Pending" {
		t.Errorf("the pending page read back as %+v, want the columns the query selects for it", a.Candidate)
	}
	if a.Published.PublishedAt == nil || !a.Published.PublishedAt.Equal(live) {
		t.Errorf("the live page's publish time read back as %v, want %v", a.Published.PublishedAt, live)
	}
	if a.Candidate.PublishedAt != nil {
		t.Errorf("a page that has never been live reported a publish time of %v", a.Candidate.PublishedAt)
	}
	if a.Published.FetchedAt.Location() != time.UTC {
		t.Errorf("a time read back in %v, want UTC", a.Published.FetchedAt.Location())
	}
}

// An address holding only one of the two states must report the other as
// absent rather than as an empty page, because a caller checks for nil.
func TestAnAddressHoldingOnlyOneStateReportsTheOtherAsAbsent(t *testing.T) {
	db, mock := newMock(t)
	mock.ExpectQuery(frag("FROM address_settings")).
		WillReturnRows(sqlmock.NewRows([]string{
			"url", "included",
			"id", "title", "content_hash", "fetched_at", "published_at",
			"id", "title", "content_hash", "fetched_at", "published_at",
		}).AddRow("/only-pending", false,
			nil, nil, nil, nil, nil,
			int64(9), nil, "h", testTime, nil))

	got, err := db.AddressStates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Published != nil {
		t.Errorf("an address with no live page reported one: %+v", got[0].Published)
	}
	if got[0].Candidate == nil {
		t.Fatal("the pending page was reported absent")
	}
	if got[0].Candidate.Title != "" {
		t.Errorf("an absent title read back as %q, want empty", got[0].Candidate.Title)
	}
	if got[0].Included {
		t.Error("a recorded exclusion was reported as included")
	}
}
