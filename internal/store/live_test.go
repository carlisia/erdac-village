// The live tier for the store. Requires a running server and is skipped when
// TEST_MYSQL_DSN is unset, and under -short, so the fast tier stays free of
// any database even on a machine where a server happens to be running.
//
// These assert what a mock driver cannot: that publish really does promote
// together and leave nothing behind, that a judgement really does survive a
// re-fetch, and that a vector of the declared width really does round-trip.
// A fake would only agree with this file's beliefs about MySQL.
//
// Every test name begins with Live so the check script can select them.
package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/carlisia/erdac-village/internal/config"
	"github.com/go-sql-driver/mysql"
)

// openLive creates a scratch database, migrates it, and returns a store scoped
// to it. The database is dropped when the test ends.
//
// The scratch name goes into the connection string rather than being selected
// with USE, because USE binds to the one pooled connection it ran on and a
// later query can arrive on a connection with no database selected at all.
func openLive(t *testing.T) *DB {
	t.Helper()
	if testing.Short() {
		t.Skip("live tier does not run under -short")
	}
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run the live tier")
	}
	ctx := context.Background()

	name := scratchName(t)
	admin, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("reach the server: %v", err)
	}
	defer admin.Close()
	if _, err := admin.db.ExecContext(ctx, "DROP DATABASE IF EXISTS "+name); err != nil {
		t.Fatalf("drop the scratch database: %v", err)
	}
	if _, err := admin.db.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create the scratch database: %v", err)
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse the connection string: %v", err)
	}
	cfg.DBName = name
	db, err := Open(ctx, cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open the scratch database: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		cleanup, err := Open(context.Background(), dsn)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.db.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name)
	})

	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// scratchName derives a database name from the test's own name, so a failure
// names the test that produced it. It refuses to truncate: two tests silently
// sharing one database would make their results depend on running order.
func scratchName(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("village_store_")
	for _, r := range strings.ToLower(t.Name()) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if len(name) > 64 {
		t.Fatalf("scratch database name %q is %d characters, over MySQL's 64-character limit; shorten the test name",
			name, len(name))
	}
	return name
}

var liveTime = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

func candidate(t *testing.T, db *DB, url, hash string) int64 {
	t.Helper()
	id, err := db.UpsertCandidate(context.Background(), Page{
		URL: url, Title: "T", Markdown: "body", ContentHash: hash, FetchedAt: liveTime,
	})
	if err != nil {
		t.Fatalf("store candidate %s: %v", url, err)
	}
	return id
}

// Applying the schema twice must leave one of everything. Migrations are not
// transactional, so re-running is the only recovery from a failure part-way
// through, and it only works if the second run is a no-op.
func TestLiveMigrateIsIdempotent(t *testing.T) {
	db := openLive(t)
	ran, err := db.Migrate(context.Background())
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if len(ran) != 0 {
		t.Errorf("a second migrate ran %d migrations, want 0", len(ran))
	}
}

// The whole review workflow, end to end: a live page keeps answering while a
// candidate is reviewed, publish promotes it, and the page it replaced is gone
// rather than merely superseded.
func TestLivePublishPromotesAndDeletesTogether(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()

	candidate(t, db, "/one", "v1")
	candidate(t, db, "/two", "p1")
	if _, err := db.Publish(ctx, liveTime); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	// A refresh replaces one page and leaves the other alone.
	candidate(t, db, "/one", "v2")
	states, err := db.AddressStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var about AddressState
	for _, s := range states {
		if s.URL == "/one" {
			about = s
		}
	}
	if about.Published == nil || about.Candidate == nil {
		t.Fatalf("/one does not hold a live page and a pending one at once: %+v", about)
	}
	if about.Published.ContentHash != "v1" || about.Candidate.ContentHash != "v2" {
		t.Errorf("/one holds %q live and %q pending, want v1 and v2",
			about.Published.ContentHash, about.Candidate.ContentHash)
	}

	got, err := db.Publish(ctx, liveTime.Add(time.Hour))
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if got.Promoted != 1 || got.Retired != 1 {
		t.Errorf("second publish reported %+v, want one promoted and one retired", got)
	}

	corpus, err := db.LiveCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if corpus.Pages != 2 {
		t.Errorf("%d pages are live, want 2: the replaced page was not deleted", corpus.Pages)
	}
	states, err = db.AddressStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.URL == "/one" && (s.Published == nil || s.Published.ContentHash != "v2") {
			t.Errorf("/one is live as %+v, want the promoted candidate", s.Published)
		}
		if s.Candidate != nil {
			t.Errorf("%s still holds a candidate after publish", s.URL)
		}
	}
}

// The judgement is held against the address rather than the page, so a page
// fetched after the judgement was made inherits it with nothing to carry
// forward. This is the assertion that a future upsert cannot quietly break.
func TestLiveExclusionSurvivesARefetch(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()

	candidate(t, db, "/three", "t1")
	if err := db.SetAddressIncluded(ctx, "/three", false, liveTime); err != nil {
		t.Fatal(err)
	}
	candidate(t, db, "/three", "t2")

	states, err := db.AddressStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.URL == "/three" && s.Included {
			t.Fatal("a re-fetch reset the exclusion, so the judgement is not sticky")
		}
	}

	got, err := db.Publish(ctx, liveTime)
	if err != nil {
		t.Fatal(err)
	}
	if got.Promoted != 0 {
		t.Errorf("publish promoted %d candidates on an excluded address", got.Promoted)
	}
	// The candidate is left alone rather than deleted, so re-including the
	// address recovers a page that was never reviewed.
	states, err = db.AddressStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if states[0].Candidate == nil {
		t.Error("publish destroyed the candidate on an excluded address")
	}
	corpus, err := db.LiveCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if corpus.Pages != 0 {
		t.Errorf("%d pages are live, want 0", corpus.Pages)
	}
}

// Excluding an address takes effect at once rather than at the next publish,
// so a page already live stops being part of the corpus immediately.
func TestLiveExcludingAPublishedAddressTakesEffectAtOnce(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()

	candidate(t, db, "/live", "h")
	if _, err := db.Publish(ctx, liveTime); err != nil {
		t.Fatal(err)
	}
	// The error is checked before the count is judged. Discarding it would
	// leave the count at zero when the query failed, so the test would report a
	// wrong corpus size and send the reader to the publish logic when the truth
	// is that nothing was counted at all.
	corpus, err := db.LiveCounts(ctx)
	if err != nil {
		t.Fatalf("count the corpus: %v", err)
	}
	if corpus.Pages != 1 {
		t.Fatalf("setup left %d live pages, want 1", corpus.Pages)
	}

	if err := db.SetAddressIncluded(ctx, "/live", false, liveTime); err != nil {
		t.Fatal(err)
	}
	corpus, err = db.LiveCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if corpus.Pages != 0 {
		t.Errorf("%d pages are still in the corpus after the address was excluded, want 0", corpus.Pages)
	}
}

// An address can carry a judgement before it has ever been fetched, so the
// listing must not be driven by the pages alone.
func TestLiveAJudgementWithNoPageIsReported(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()
	if err := db.SetAddressIncluded(ctx, "/never-fetched", false, liveTime); err != nil {
		t.Fatal(err)
	}
	states, err := db.AddressStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].URL != "/never-fetched" || states[0].Included {
		t.Fatalf("a judgement on an unfetched address was not reported: %+v", states)
	}
}

// Two crawls interleaving would produce one candidate set mixing two runs, so
// the second must be refused; a crashed run must not block the system forever.
//
// The lock's age is measured on the server's clock, which no test can move, so
// an abandoned lock is simulated by putting its start time in the past. That
// is the same state a crashed run leaves behind.
func TestLiveFetchLock(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()

	if err := db.AcquireFetchLock(ctx, "run-1"); err != nil {
		t.Fatalf("first fetch could not take the lock: %v", err)
	}
	if err := db.AcquireFetchLock(ctx, "run-2"); !errors.Is(err, ErrFetchInProgress) {
		t.Fatalf("a second fetch got %v, want ErrFetchInProgress", err)
	}
	if _, err := db.Publish(ctx, liveTime); !errors.Is(err, ErrPublishDuringFetch) {
		t.Fatalf("publish during a fetch got %v, want ErrPublishDuringFetch", err)
	}

	// A publish timestamped far in the future must still be refused. The lock
	// is judged on the server's clock, so a caller cannot argue a running
	// fetch into looking abandoned by choosing the time it passes in.
	if _, err := db.Publish(ctx, liveTime.Add(1000*FetchLockTTL)); !errors.Is(err, ErrPublishDuringFetch) {
		t.Fatalf("a publish stamped far ahead got %v, want ErrPublishDuringFetch", err)
	}

	if _, err := db.db.ExecContext(ctx,
		`UPDATE system_state SET fetch_started_at = UTC_TIMESTAMP(6) - INTERVAL ? SECOND WHERE id = 1`,
		int(FetchLockTTL.Seconds())+60); err != nil {
		t.Fatal(err)
	}
	if err := db.AcquireFetchLock(ctx, "run-3"); err != nil {
		t.Fatalf("an abandoned lock was not taken over: %v", err)
	}
	if err := db.ReleaseFetchLock(ctx, "run-1", &liveTime); !errors.Is(err, ErrFetchLockLost) {
		t.Fatalf("the overrun run released a lock it no longer held: %v", err)
	}
	if err := db.ReleaseFetchLock(ctx, "run-3", &liveTime); err != nil {
		t.Fatalf("release: %v", err)
	}

	state, err := db.SystemState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.FetchLock != "" {
		t.Errorf("the lock is still held by %q after release", state.FetchLock)
	}
	if state.LastFetchedAt == nil || !state.LastFetchedAt.Equal(liveTime) {
		t.Errorf("the fetch time was recorded as %v, want %v", state.LastFetchedAt, liveTime)
	}
}

// The seeded row must exist and must read back as the defaults the schema
// declares, because every later check reads it rather than assuming it.
func TestLiveSystemStateIsSeeded(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()
	state, err := db.SystemState(ctx)
	if err != nil {
		t.Fatalf("the migration did not seed the system state: %v", err)
	}
	if state.Halted || state.SessionEpoch != 0 || state.LastPublishedAt != nil {
		t.Errorf("a freshly migrated database reports %+v", state)
	}

	if err := db.SetHalted(ctx, true); err != nil {
		t.Fatal(err)
	}
	epoch, err := db.ClearConversations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if epoch != 1 {
		t.Errorf("clearing conversations moved the epoch to %d, want 1", epoch)
	}
	state, err = db.SystemState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Halted || state.SessionEpoch != 1 {
		t.Errorf("after halting and clearing: %+v", state)
	}
}

// The embedding round-trips through Go as a string literal because the two
// extensions do not compose. This asserts the route the ingest path takes, and
// that deleting the page takes the chunks and their vectors with it.
func TestLiveEmbeddingsRoundTripAndCascade(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()

	id := candidate(t, db, "/e", "h")
	ids, err := db.ReplaceChunks(ctx, id, []Chunk{
		{Ordinal: 0, HeadingPath: "A > B", Text: "first", TokenCount: 1},
		{Ordinal: 1, Text: "second", TokenCount: 1},
	})
	if err != nil {
		t.Fatalf("replace chunks: %v", err)
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("replace chunks returned %v, want two distinct identifiers", ids)
	}

	vector := realisticVector()
	if err := db.StoreEmbeddings(ctx, []Embedding{{ChunkID: ids[0], Vector: vector}}); err != nil {
		t.Fatalf("store embedding: %v", err)
	}
	// Storing again for the same chunk must replace rather than be refused,
	// because a re-embedding of an unchanged chunk is an ordinary event.
	if err := db.StoreEmbeddings(ctx, []Embedding{{ChunkID: ids[0], Vector: vector}}); err != nil {
		t.Fatalf("re-store embedding: %v", err)
	}

	// Replacing the chunks must not leave the previous run's rows behind.
	if _, err := db.ReplaceChunks(ctx, id, []Chunk{{Ordinal: 0, Text: "only", TokenCount: 1}}); err != nil {
		t.Fatal(err)
	}
	var chunks, embeddings int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&chunks); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings`).Scan(&embeddings); err != nil {
		t.Fatal(err)
	}
	if chunks != 1 || embeddings != 0 {
		t.Errorf("after replacing: %d chunks and %d embeddings, want 1 and 0", chunks, embeddings)
	}
}

// The atomicity the fast tier could only gesture at. The second vector names a
// chunk that does not exist, so the server refuses it, and the first must not
// survive: a page embedded in part answers from part of itself while the run
// that wrote it reports a failure.
func TestLiveAFailedEmbeddingLeavesNoneBehind(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()
	id := candidate(t, db, "/four", "h")
	ids, err := db.ReplaceChunks(ctx, id, []Chunk{{Ordinal: 0, Text: "t", TokenCount: 1}})
	if err != nil {
		t.Fatal(err)
	}
	vector := make([]float32, config.MaxVectorDimensions)

	err = db.StoreEmbeddings(ctx, []Embedding{
		{ChunkID: ids[0], Vector: vector},
		{ChunkID: ids[0] + 100000, Vector: vector},
	})
	if err == nil {
		t.Fatal("an embedding for a chunk that does not exist was accepted")
	}

	var stored int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM embeddings`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 0 {
		t.Errorf("%d embeddings survived a failed run, want 0: the page is half embedded", stored)
	}
}

// A vector of any other width must be refused, because the column is declared
// at exactly one width and nothing narrows or widens one.
func TestLiveAWrongWidthVectorIsRefusedByTheColumn(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()
	id := candidate(t, db, "/w", "h")
	ids, err := db.ReplaceChunks(ctx, id, []Chunk{{Ordinal: 0, Text: "t", TokenCount: 1}})
	if err != nil {
		t.Fatal(err)
	}
	narrow := "[" + strings.Repeat("0.1,", 9) + "0.1]"
	_, err = db.db.ExecContext(ctx,
		`INSERT INTO embeddings (chunk_id, embedding) VALUES (?, '`+narrow+`')`, ids[0])
	if err == nil {
		t.Fatal("a ten-element vector was accepted into a column declared far wider")
	}
}

// Times are written explicitly in UTC and must read back as the same instant,
// which is what no column defaulting to a clock is there to guarantee.
func TestLiveTimesRoundTripAsUTC(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()
	candidate(t, db, "/t", "h")

	states, err := db.AddressStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("got %d addresses, want 1", len(states))
	}
	got := states[0].Candidate.FetchedAt
	if !got.Equal(liveTime) {
		t.Errorf("the fetch time read back as %v, want %v", got, liveTime)
	}
	if got.Location() != time.UTC {
		t.Errorf("the fetch time read back in %v, want UTC", got.Location())
	}
}

// The server must refuse an address wider than the column rather than truncate
// it, and the refusal must reach a caller as the same error the local check
// produces. This is the pairing the fast tier cannot assert on its own.
func TestLiveOverlongAddressReachesTheCallerAsOneError(t *testing.T) {
	db := openLive(t)
	_, err := db.UpsertCandidate(context.Background(), Page{
		URL: "/" + strings.Repeat("a", MaxAddressLength), Markdown: "b", ContentHash: "h", FetchedAt: liveTime,
	})
	if !errors.Is(err, ErrAddressTooLong) {
		t.Fatalf("got %v, want ErrAddressTooLong", err)
	}
}

// Publish asks for an isolation level rather than inheriting the server's,
// because locking the candidate set only keeps a candidate written mid-publish
// from being left behind where a range lock also blocks an insert into the
// range it covers. This asserts the request is actually honoured: a server may
// have a default, and a driver may carry a session setting, and neither is a
// promise that the transaction got what it asked for.
func TestLivePublishGetsTheIsolationLevelItAsksFor(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()

	tx, err := db.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatalf("the server refused the isolation level publish asks for: %v", err)
	}
	defer tx.Rollback()

	var level string
	if err := tx.QueryRowContext(ctx, `SELECT @@transaction_isolation`).Scan(&level); err != nil {
		t.Fatal(err)
	}
	if level != "REPEATABLE-READ" {
		t.Errorf("publish asked for repeatable read and the transaction reports %q; "+
			"the locking comment in Publish no longer holds", level)
	}
}

// The store opens no unbounded pool. The model functions hold a server thread
// for the whole outbound call, so an unbounded pool exhausts the server's
// connection limit under load rather than making callers wait.
func TestLiveThePoolIsBounded(t *testing.T) {
	db := openLive(t)
	stats := db.db.Stats()
	if stats.MaxOpenConnections <= 0 {
		t.Fatalf("the pool reports %d as its maximum, which means unlimited",
			stats.MaxOpenConnections)
	}
	var serverMax int
	if err := db.db.QueryRowContext(context.Background(),
		`SELECT @@max_connections`).Scan(&serverMax); err != nil {
		t.Fatal(err)
	}
	if stats.MaxOpenConnections >= serverMax {
		t.Errorf("the pool may open %d connections and the server accepts %d in total, "+
			"so one process can exhaust it", stats.MaxOpenConnections, serverMax)
	}
}

// realisticVector is shaped like an embedding rather than like a convenient
// constant: negative elements, a zero, and magnitudes small enough that the
// shortest representation of a float would use exponent notation.
//
// An earlier version of this tier filled every element with one positive value
// just above a tenth. That proved the round trip for numbers a normalised
// embedding almost never contains, and it was the only live coverage the
// vector write had, so a formatting choice that emitted exponents passed it.
func realisticVector() []float32 {
	v := make([]float32, config.MaxVectorDimensions)
	pattern := []float32{0.1, -0.1, 0.0123, -0.00456, 1.2e-05, -3.4e-06, 0, 1e-08}
	for i := range v {
		v[i] = pattern[i%len(pattern)]
	}
	return v
}

// The vector the ingest path actually writes must survive the round trip, and
// the elements that matter are the awkward ones. This reads the column back
// and compares, rather than only checking the write was accepted.
func TestLiveARealisticVectorRoundTrips(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()

	id := candidate(t, db, "/five", "h")
	ids, err := db.ReplaceChunks(ctx, id, []Chunk{{Ordinal: 0, Text: "t", TokenCount: 1}})
	if err != nil {
		t.Fatal(err)
	}
	want := realisticVector()
	if err := db.StoreEmbeddings(ctx, []Embedding{{ChunkID: ids[0], Vector: want}}); err != nil {
		t.Fatalf("a vector with negative and very small elements was refused: %v", err)
	}

	// The column arrives as the text form under both protocols, measured.
	var raw []byte
	if err := db.db.QueryRowContext(ctx,
		`SELECT embedding FROM embeddings WHERE chunk_id = ?`, ids[0]).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	read := string(raw)
	if !strings.HasPrefix(read, "[") || !strings.HasSuffix(read, "]") {
		t.Fatalf("the column did not read back as a bracketed vector: %.80s", read)
	}
	if n := strings.Count(read, ",") + 1; n != config.MaxVectorDimensions {
		t.Errorf("the column read back with %d elements, want %d", n, config.MaxVectorDimensions)
	}
	for _, sample := range []string{"-0.1", "0.000012", "-0.0000034"} {
		if !strings.Contains(read, sample) {
			t.Errorf("%q did not survive the round trip; stored: %.120s", sample, read)
		}
	}
}
