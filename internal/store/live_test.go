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
	"strconv"
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

// holdLock takes the fetch lock for the test as one run and releases it at
// cleanup if the test did not finish the fetch itself. Every candidate write
// requires the lock, so a test that writes pages starts here.
func holdLock(t *testing.T, db *DB) string {
	t.Helper()
	owner := "run-" + strings.ToLower(t.Name()[len("TestLive"):])
	if len(owner) > 60 {
		owner = owner[:60]
	}
	if err := db.AcquireFetchLock(context.Background(), owner); err != nil {
		t.Fatalf("take the fetch lock: %v", err)
	}
	t.Cleanup(func() {
		_ = db.ReleaseFetchLock(context.Background(), owner, nil, false)
	})
	return owner
}

// finishFetch releases the lock as a completed run that visited every page,
// which is the state a publish needs to find.
func finishFetch(t *testing.T, db *DB, owner string) {
	t.Helper()
	if err := db.ReleaseFetchLock(context.Background(), owner, &liveTime, true); err != nil {
		t.Fatalf("finish the fetch: %v", err)
	}
}

func candidate(t *testing.T, db *DB, owner, url, hash string) int64 {
	t.Helper()
	return candidateWith(t, db, owner, Page{
		URL: url, Title: "T", Markdown: "body", ContentHash: hash, FetchedAt: liveTime,
	}, nil, nil)
}

func candidateWith(t *testing.T, db *DB, owner string, p Page, chunks []Chunk, vectors [][]float32) int64 {
	t.Helper()
	id, err := db.StoreCandidate(context.Background(), owner, p, chunks, vectors)
	if err != nil {
		t.Fatalf("store candidate %s: %v", p.URL, err)
	}
	return id
}

func count(t *testing.T, db *DB, table string) int {
	t.Helper()
	var n int
	if err := db.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
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

	owner := holdLock(t, db)
	candidate(t, db, owner, "/one", "v1")
	candidate(t, db, owner, "/two", "p1")
	finishFetch(t, db, owner)
	if _, err := db.Publish(ctx, liveTime); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	// A refresh replaces one page and leaves the other alone.
	owner = holdLock(t, db)
	candidate(t, db, owner, "/one", "v2")
	finishFetch(t, db, owner)

	states, err := db.AddressStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var one AddressState
	for _, s := range states {
		if s.URL == "/one" {
			one = s
		}
	}
	if one.Published == nil || one.Candidate == nil {
		t.Fatalf("/one does not hold a live page and a pending one at once: %+v", one)
	}
	if one.Published.ContentHash != "v1" || one.Candidate.ContentHash != "v2" {
		t.Errorf("/one holds %q live and %q pending, want v1 and v2",
			one.Published.ContentHash, one.Candidate.ContentHash)
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
// forward.
func TestLiveExclusionSurvivesARefetch(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()

	owner := holdLock(t, db)
	candidate(t, db, owner, "/three", "t1")
	if err := db.SetAddressIncluded(ctx, "/three", false, liveTime); err != nil {
		t.Fatal(err)
	}
	candidate(t, db, owner, "/three", "t2")
	finishFetch(t, db, owner)

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

// Excluding an address takes effect at once rather than at the next publish.
func TestLiveExcludingALivePageIsImmediate(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()

	owner := holdLock(t, db)
	candidate(t, db, owner, "/live", "h")
	finishFetch(t, db, owner)
	if _, err := db.Publish(ctx, liveTime); err != nil {
		t.Fatal(err)
	}
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

// An address can carry a judgement before it has ever been fetched.
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

// The lock, all three parts: a second fetch is refused, a slow one renews and
// keeps its place, a crashed one is taken over and the takeover is recorded,
// the run that lost the lock can neither renew nor release it, publish waits
// for a completed force run, and a completed refresh is not enough.
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
	if _, err := db.Publish(ctx, liveTime.Add(1000*FetchLockTTL)); !errors.Is(err, ErrPublishDuringFetch) {
		t.Fatalf("a publish stamped far ahead got %v, want ErrPublishDuringFetch", err)
	}

	// A slow run renews. Its start moves to now, so it is no longer near expiry.
	if _, err := db.db.ExecContext(ctx,
		`UPDATE system_state SET fetch_started_at = UTC_TIMESTAMP(6) - INTERVAL ? SECOND WHERE id = 1`,
		int(FetchLockTTL.Seconds())-60); err != nil {
		t.Fatal(err)
	}
	if err := db.RenewFetchLock(ctx, "run-1"); err != nil {
		t.Fatalf("a live run could not renew: %v", err)
	}
	state, err := db.SystemState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.FetchStartedAt == nil || time.Since(*state.FetchStartedAt) > time.Minute {
		t.Errorf("renewal did not move the start time to now: %v", state.FetchStartedAt)
	}

	// The run crashes. Its lock ages past the expiry.
	if _, err := db.db.ExecContext(ctx,
		`UPDATE system_state SET fetch_started_at = UTC_TIMESTAMP(6) - INTERVAL ? SECOND WHERE id = 1`,
		int(FetchLockTTL.Seconds())+60); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Publish(ctx, liveTime); !errors.Is(err, ErrAbandonedFetch) {
		t.Fatalf("publish over an abandoned lock got %v, want ErrAbandonedFetch", err)
	}
	if err := db.AcquireFetchLock(ctx, "run-3"); err != nil {
		t.Fatalf("an abandoned lock was not taken over: %v", err)
	}
	state, err = db.SystemState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.FetchTakenOverAt == nil {
		t.Fatal("the takeover was not recorded")
	}

	// The crashed run, if it wakes, can do nothing.
	if err := db.RenewFetchLock(ctx, "run-1"); !errors.Is(err, ErrFetchLockLost) {
		t.Errorf("the overrun run renewed a lock it no longer held: %v", err)
	}
	if err := db.ReleaseFetchLock(ctx, "run-1", &liveTime, true); !errors.Is(err, ErrFetchLockLost) {
		t.Errorf("the overrun run released a lock it no longer held: %v", err)
	}
	if _, err := db.StoreCandidate(ctx, "run-1", Page{URL: "/late", Markdown: "b", ContentHash: "h", FetchedAt: liveTime}, nil, nil); !errors.Is(err, ErrFetchLockLost) {
		t.Errorf("the overrun run wrote a page after losing the lock: %v", err)
	}

	// The new run completes as a refresh. That is not enough to clear the
	// takeover, because a refresh skips pages.
	if err := db.ReleaseFetchLock(ctx, "run-3", &liveTime, false); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := db.Publish(ctx, liveTime); !errors.Is(err, ErrTakeoverPending) {
		t.Fatalf("publish after a takeover and a refresh got %v, want ErrTakeoverPending", err)
	}

	// A force run completes. Taking the free lock records no new takeover, and
	// finishing in force mode clears the one that stands.
	if err := db.AcquireFetchLock(ctx, "run-4"); err != nil {
		t.Fatal(err)
	}
	if err := db.ReleaseFetchLock(ctx, "run-4", &liveTime, true); err != nil {
		t.Fatal(err)
	}
	state, err = db.SystemState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.FetchLock != "" || state.FetchTakenOverAt != nil {
		t.Errorf("after a completed force run: lock %q, takeover %v; want neither", state.FetchLock, state.FetchTakenOverAt)
	}
	if state.LastFetchedAt == nil || !state.LastFetchedAt.Equal(liveTime) {
		t.Errorf("the fetch time was recorded as %v, want %v", state.LastFetchedAt, liveTime)
	}
	if _, err := db.Publish(ctx, liveTime); err != nil {
		t.Fatalf("publish after a completed force run: %v", err)
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
	if state.Halted || state.SessionEpoch != 0 || state.LastPublishedAt != nil || state.FetchTakenOverAt != nil {
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

// A candidate arrives with its chunks and their vectors in one write.
// Re-storing replaces the chunks rather than adding to them, and deleting the
// page takes chunks and vectors with it through two separate cascades.
func TestLiveACandidateIsStoredWithChunksAndVectors(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()
	owner := holdLock(t, db)

	v := realisticVector()
	id := candidateWith(t, db, owner, Page{URL: "/e", Markdown: "b", ContentHash: "h", FetchedAt: liveTime},
		[]Chunk{{Ordinal: 0, HeadingPath: "A > B", Text: "first", TokenCount: 1}, {Ordinal: 1, Text: "second", TokenCount: 1}},
		[][]float32{v, v})
	if count(t, db, "chunks") != 2 || count(t, db, "embeddings") != 2 {
		t.Fatalf("stored %d chunks and %d embeddings, want 2 and 2", count(t, db, "chunks"), count(t, db, "embeddings"))
	}

	again := candidateWith(t, db, owner, Page{URL: "/e", Markdown: "b2", ContentHash: "h2", FetchedAt: liveTime},
		[]Chunk{{Ordinal: 0, Text: "only", TokenCount: 1}}, [][]float32{v})
	if again != id {
		t.Errorf("re-storing the same address produced page %d, want %d", again, id)
	}
	if count(t, db, "chunks") != 1 || count(t, db, "embeddings") != 1 {
		t.Errorf("after re-storing: %d chunks and %d embeddings, want 1 and 1", count(t, db, "chunks"), count(t, db, "embeddings"))
	}

	if _, err := db.db.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "chunks") != 0 || count(t, db, "embeddings") != 0 {
		t.Errorf("after deleting the page: %d chunks and %d embeddings survived", count(t, db, "chunks"), count(t, db, "embeddings"))
	}
}

// A write that fails on its last statement leaves nothing of the page: no
// candidate, no chunks, no vectors. Two chunks with one ordinal trip the
// uniqueness rule on the second insert, which is a real refusal from the
// server rather than a stubbed one.
func TestLiveAFailedWriteLeavesNothingOfThePage(t *testing.T) {
	db := openLive(t)
	owner := holdLock(t, db)
	v := realisticVector()
	_, err := db.StoreCandidate(context.Background(), owner,
		Page{URL: "/half", Markdown: "b", ContentHash: "h", FetchedAt: liveTime},
		[]Chunk{{Ordinal: 0, Text: "a", TokenCount: 1}, {Ordinal: 0, Text: "b", TokenCount: 1}},
		[][]float32{v, v})
	if err == nil {
		t.Fatal("two chunks with one ordinal were accepted")
	}
	if count(t, db, "documents") != 0 || count(t, db, "chunks") != 0 || count(t, db, "embeddings") != 0 {
		t.Errorf("a failed write left %d documents, %d chunks, %d embeddings; want none",
			count(t, db, "documents"), count(t, db, "chunks"), count(t, db, "embeddings"))
	}
}

// The lock is no longer advisory. A write with no lock held, and a write from
// a run other than the holder, are both refused by the store.
func TestLiveAWriteWithoutTheLockIsRefused(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()
	page := Page{URL: "/x", Markdown: "b", ContentHash: "h", FetchedAt: liveTime}

	if _, err := db.StoreCandidate(ctx, "nobody", page, nil, nil); !errors.Is(err, ErrFetchLockLost) {
		t.Fatalf("a write with no lock held got %v, want ErrFetchLockLost", err)
	}
	holdLock(t, db)
	if _, err := db.StoreCandidate(ctx, "someone-else", page, nil, nil); !errors.Is(err, ErrFetchLockLost) {
		t.Fatalf("a write by a run that is not the holder got %v, want ErrFetchLockLost", err)
	}
	if count(t, db, "documents") != 0 {
		t.Error("a refused write left a document behind")
	}
}

// A vector of any other width must be refused by the column, because it is
// declared at exactly one width and nothing narrows or widens one.
func TestLiveAWrongWidthVectorIsRefusedByTheColumn(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()
	owner := holdLock(t, db)
	id := candidate(t, db, owner, "/w", "h")
	res, err := db.db.ExecContext(ctx,
		`INSERT INTO chunks (document_id, ordinal, text, token_count) VALUES (?, 0, 't', 1)`, id)
	if err != nil {
		t.Fatal(err)
	}
	chunkID, _ := res.LastInsertId()
	narrow := "[" + strings.Repeat("0.1,", 9) + "0.1]"
	if _, err := db.db.ExecContext(ctx,
		`INSERT INTO embeddings (chunk_id, embedding) VALUES (?, '`+narrow+`')`, chunkID); err == nil {
		t.Fatal("a ten-element vector was accepted into a column declared far wider")
	}
}

// Times are written explicitly in UTC and must read back as the same instant.
// The sitemap's last-modified date rides along and must survive too.
func TestLiveTimesRoundTripAsUTC(t *testing.T) {
	db := openLive(t)
	owner := holdLock(t, db)
	lastMod := liveTime.Add(-48 * time.Hour)
	candidateWith(t, db, owner, Page{URL: "/t", Markdown: "b", ContentHash: "h", FetchedAt: liveTime, LastMod: &lastMod}, nil, nil)

	states, err := db.AddressStates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Candidate == nil {
		t.Fatalf("got %+v, want one address with a candidate", states)
	}
	c := states[0].Candidate
	if !c.FetchedAt.Equal(liveTime) || c.FetchedAt.Location() != time.UTC {
		t.Errorf("the fetch time read back as %v, want %v in UTC", c.FetchedAt, liveTime)
	}
	if c.LastMod == nil || !c.LastMod.Equal(lastMod) {
		t.Errorf("the last-modified date read back as %v, want %v", c.LastMod, lastMod)
	}
}

// An address wider than the column is refused before the server sees it.
func TestLiveOverlongAddressReachesTheCallerAsOneError(t *testing.T) {
	db := openLive(t)
	_, err := db.StoreCandidate(context.Background(), "any", Page{
		URL: "/" + strings.Repeat("a", MaxAddressLength), Markdown: "b", ContentHash: "h", FetchedAt: liveTime,
	}, nil, nil)
	if !errors.Is(err, ErrAddressTooLong) {
		t.Fatalf("got %v, want ErrAddressTooLong", err)
	}
}

// providerKey gates the two tests that reach the embedding provider over the
// network. They need the database and the key; without the key they skip and
// say so, because a green run must never mean half of it did not execute.
func providerKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("VILLAGE_GOOGLE_KEY")
	if key == "" {
		t.Skip("set VILLAGE_GOOGLE_KEY to run the tests that call the embedding provider")
	}
	return key
}

// The database embeds a piece of text through a pinned connection with the
// key in a session variable, and what comes back is a vector of the declared
// width. This is the round trip the ingest path depends on, measured.
func TestLiveEmbedReturnsTheDeclaredWidth(t *testing.T) {
	db := openLive(t)
	key := providerKey(t)
	ctx := context.Background()
	cfg, err := config.Load("../../siteconfig.toml")
	if err != nil {
		t.Fatal(err)
	}
	e, err := db.OpenEmbedder(ctx, cfg.Models.EmbeddingProvider, cfg.Models.EmbeddingModel, key, 30*time.Second)
	if err != nil {
		t.Fatalf("open the embedder: %v", err)
	}
	defer e.Close()
	v, err := e.Embed(ctx, "A short sentence to embed.")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(v) != config.MaxVectorDimensions {
		t.Errorf("the embedding is %d wide, want %d", len(v), config.MaxVectorDimensions)
	}
}

// The embedding function does not raise. With a key the provider rejects it
// returns nothing and a warning, and the store must notice and carry the
// warning's words, because that is the only account of what went wrong.
func TestLiveANullEmbeddingIsNoticed(t *testing.T) {
	db := openLive(t)
	providerKey(t)
	ctx := context.Background()
	cfg, err := config.Load("../../siteconfig.toml")
	if err != nil {
		t.Fatal(err)
	}
	e, err := db.OpenEmbedder(ctx, cfg.Models.EmbeddingProvider, cfg.Models.EmbeddingModel, "not-a-real-key", 30*time.Second)
	if err != nil {
		t.Fatalf("open the embedder: %v", err)
	}
	defer e.Close()
	_, err = e.Embed(ctx, "A short sentence to embed.")
	if !errors.Is(err, ErrEmbeddingFailed) {
		t.Fatalf("a rejected key got %v, want ErrEmbeddingFailed", err)
	}
	if !strings.Contains(err.Error(), "Warning") && !strings.Contains(err.Error(), "Error") {
		t.Errorf("the failure does not carry the server's warning: %v", err)
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

	owner := holdLock(t, db)
	want := realisticVector()
	id := candidateWith(t, db, owner, Page{URL: "/five", Markdown: "b", ContentHash: "h", FetchedAt: liveTime},
		[]Chunk{{Ordinal: 0, Text: "t", TokenCount: 1}}, [][]float32{want})
	var chunkID int64
	if err := db.db.QueryRowContext(ctx, `SELECT id FROM chunks WHERE document_id = ?`, id).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}
	ids := []int64{chunkID}

	// The column arrives as the text form under both protocols, measured. The
	// server spells the elements its own way on the way out: the first live run
	// showed 0.000012 coming back as 1.20000004e-05, the same float32 value
	// written as the server prefers. So the comparison is numeric, element by
	// element, at float32 precision. Comparing the spelling asserted something
	// the round trip never promised.
	var raw []byte
	if err := db.db.QueryRowContext(ctx,
		`SELECT embedding FROM embeddings WHERE chunk_id = ?`, ids[0]).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	read := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(read, "[") || !strings.HasSuffix(read, "]") {
		t.Fatalf("the column did not read back as a bracketed vector: %.80s", read)
	}
	fields := strings.Split(strings.Trim(read, "[]"), ",")
	if len(fields) != len(want) {
		t.Fatalf("the column read back with %d elements, want %d", len(fields), len(want))
	}
	for i, f := range fields {
		got, err := strconv.ParseFloat(strings.TrimSpace(f), 32)
		if err != nil {
			t.Fatalf("element %d read back as %q, which is not a number: %v", i, f, err)
		}
		if float32(got) != want[i] {
			t.Errorf("element %d: wrote %v, read back %v", i, want[i], float32(got))
		}
	}
}

// The column is declared at one width and the extension exposes a function
// that reports a stored value's actual width. The two must agree for every
// row, and this asks the server rather than trusting the declaration. It is
// also the only way to check: LENGTH, CONVERT and JSON_LENGTH all refuse a
// vector value, measured, so a reader who reaches for them gets an error
// rather than a number.
func TestLiveEveryStoredVectorIsTheDeclaredWidth(t *testing.T) {
	db := openLive(t)
	ctx := context.Background()
	owner := holdLock(t, db)
	v := realisticVector()
	candidateWith(t, db, owner, Page{URL: "/w", Markdown: "b", ContentHash: "h", FetchedAt: liveTime},
		[]Chunk{{Ordinal: 0, Text: "a", TokenCount: 1}, {Ordinal: 1, Text: "b", TokenCount: 1}}, [][]float32{v, v})

	rows, err := db.db.QueryContext(ctx,
		`SELECT VECTOR_DIMENSION(embedding) AS width, COUNT(*) FROM embeddings GROUP BY width`)
	if err != nil {
		t.Fatalf("ask the server for stored widths: %v", err)
	}
	defer rows.Close()
	var seen int
	for rows.Next() {
		var width, n int
		if err := rows.Scan(&width, &n); err != nil {
			t.Fatal(err)
		}
		seen += n
		if width != config.MaxVectorDimensions {
			t.Errorf("%d stored vectors are %d wide, want %d", n, width, config.MaxVectorDimensions)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Errorf("the server reported widths for %d vectors, want 2", seen)
	}
}
