// The live-tier tests for the schema these migrations create.
//
// They live beside the migrations rather than in a package of their own,
// because the package that holds the schema is the package that should prove
// it. An earlier arrangement put them in a package named for the schema, and
// when the SQL moved here that package was left named for something it no
// longer contained, with no rule saying which of two live-tier homes a new
// test belonged in.
//
// These require a running server and are skipped when TEST_MYSQL_DSN is unset.
// A fake cannot prove any of what they assert: every rule below is enforced by
// the database, not by Go, so a test that substituted the database would only
// be asserting this file's own beliefs about MySQL.
//
// Every test name begins with Live so the check script can select them.
package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/carlisia/erdac-village/internal/config"
	_ "github.com/go-sql-driver/mysql"
)

// scratchDSN puts the scratch database into the connection string itself.
//
// It cannot be selected with USE. database/sql hands out pooled connections,
// and USE binds to the one connection it ran on, so a later query can arrive
// on a connection that has no database selected at all. The same hazard
// applies to SET time_zone, which is why the handle below is also pinned to a
// single connection.
func scratchDSN(t *testing.T, dsn, name string) string {
	t.Helper()
	base, params, hasParams := strings.Cut(dsn, "?")
	if !strings.HasSuffix(base, "/") {
		t.Fatalf("TEST_MYSQL_DSN must end with / and name no database, got %q", dsn)
	}
	out := base + name + "?parseTime=true&loc=UTC&multiStatements=true&time_zone=%27%2B00%3A00%27"
	if hasParams {
		out += "&" + params
	}
	return out
}

// scratchName derives a database name from the test's own name so a failure
// names the test that produced it. It refuses to truncate: two tests silently
// sharing one database would make their results depend on running order.
func scratchName(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("village_schema_")
	for _, r := range strings.ToLower(t.Name()) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if len(name) > 64 {
		t.Fatalf("scratch database name %q is %d characters, over MySQL's 64-character limit; shorten the test name", name, len(name))
	}
	return name
}

// open creates a scratch database, applies the schema, and returns a handle
// scoped to it. The database is dropped when the test ends.
//
// The live tier is skipped under -short as well as when the connection string
// is absent, so the fast tier stays free of any database even on a machine
// where a server happens to be running.
func open(t *testing.T) *sql.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("live tier does not run under -short")
	}
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run the live tier")
	}
	name := scratchName(t)

	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	mustExec(t, admin, "DROP DATABASE IF EXISTS "+name)
	mustExec(t, admin, "CREATE DATABASE "+name)

	db, err := sql.Open("mysql", scratchDSN(t, dsn, name))
	if err != nil {
		t.Fatalf("open scratch: %v", err)
	}
	// One connection, so a session setting made by a test is still in force
	// for the statement that depends on it.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		t.Fatalf("ping scratch: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		cleanup, err := sql.Open("mysql", dsn)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Exec("DROP DATABASE IF EXISTS " + name)
	})

	apply(t, db)
	return db
}

// apply runs every migration's statements directly, rather than through the
// migrator, and it takes them from the embedded set rather than from a path.
//
// Directly, because these tests assert that the SQL itself survives being run
// more than once, and the migrator would decline the second run as already
// recorded, so it would answer a different question.
//
// From the embedded set, because a hardcoded path names one file. The day a
// second migration lands, a path would leave this whole tier passing against a
// schema it is no longer testing, and passing is what a broken thing looks
// like from outside.
func apply(t *testing.T, db *sql.DB) {
	t.Helper()
	all, err := All()
	if err != nil {
		t.Fatalf("read the embedded migrations: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no migrations are embedded, so this tier would test an empty database")
	}
	for _, m := range all {
		if _, err := db.Exec(m.SQL); err != nil {
			t.Fatalf("apply migration %d (%s): %v", m.Version, m.Name, err)
		}
	}
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func insertDoc(db *sql.DB, url, hash, state string) error {
	_, err := db.Exec(
		`INSERT INTO documents (url, markdown, content_hash, state, fetched_at)
		 VALUES (?, 'body', ?, ?, UTC_TIMESTAMP(6))`, url, hash, state)
	return err
}

// The schema must survive being applied more than once, because MySQL commits
// implicitly on every schema statement, so a migration that fails part-way
// leaves the earlier statements applied and re-running is the only recovery.
func TestLiveMigrationIsIdempotent(t *testing.T) {
	db := open(t)
	apply(t, db)
	apply(t, db)

	var tables, versions int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()`,
	).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if tables != 8 {
		t.Errorf("after three applies: %d tables, want 8", tables)
	}
	if versions != 1 {
		t.Errorf("after three applies: %d migration rows, want 1", versions)
	}
}

// One address may hold a live page and a pending one at the same time. This is
// the whole point of the review workflow: the assistant keeps answering from
// the published page while an administrator reviews the candidate.
func TestLiveOneAddressHoldsPublishedAndCandidate(t *testing.T) {
	db := open(t)
	if err := insertDoc(db, "/about", "h1", "published"); err != nil {
		t.Fatalf("published page refused: %v", err)
	}
	if err := insertDoc(db, "/about", "h2", "candidate"); err != nil {
		t.Fatalf("candidate for the same address refused: %v", err)
	}
}

// The rules that must be refused. A change making any of these pass is a
// change that removed a protection.
func TestLiveDuplicateStatesAreRefused(t *testing.T) {
	db := open(t)
	for _, state := range []string{"published", "candidate"} {
		if err := insertDoc(db, "/dup", "first", state); err != nil {
			t.Fatalf("first %s page refused: %v", state, err)
		}
		err := insertDoc(db, "/dup", "second", state)
		if err == nil {
			t.Fatalf("a second %s page for one address was accepted", state)
		}
		if !strings.Contains(err.Error(), "documents_one_"+state+"_per_url") {
			t.Errorf("%s: error does not name the index that refused it: %v", state, err)
		}
	}
}

// Superseded rows exist only inside the publish transaction, and there may be
// several for one address, so they must not compete for either index.
func TestLiveSupersededRowsAreUnconstrained(t *testing.T) {
	db := open(t)
	for _, h := range []string{"a", "b", "c"} {
		if err := insertDoc(db, "/old", h, "superseded"); err != nil {
			t.Fatalf("superseded row %q refused: %v", h, err)
		}
	}
}

// Why one insert can never collide on both unique indexes: a page holds
// exactly one state, so at most one generated column is ever non-NULL. This
// asserts the structural claim rather than restating it in a comment.
func TestLiveNoRowPopulatesBothGeneratedColumns(t *testing.T) {
	db := open(t)
	for _, state := range []string{"published", "candidate", "superseded"} {
		if err := insertDoc(db, "/x-"+state, "h", state); err != nil {
			t.Fatal(err)
		}
	}
	var both int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM documents WHERE url_candidate IS NOT NULL AND url_published IS NOT NULL`,
	).Scan(&both); err != nil {
		t.Fatal(err)
	}
	if both != 0 {
		t.Errorf("%d rows populate both generated columns, want 0", both)
	}
}

// Retiring before promoting is not a stylistic preference. Promoting first
// collides with the live page for the same address, which is the failure the
// original system shipped once.
func TestLivePromoteBeforeRetireIsRefused(t *testing.T) {
	db := open(t)
	mustExec(t, db, `INSERT INTO documents (url, markdown, content_hash, state, fetched_at)
		VALUES ('/p','old','h1','published',UTC_TIMESTAMP(6)), ('/p','new','h2','candidate',UTC_TIMESTAMP(6))`)

	_, err := db.Exec(`UPDATE documents SET state='published' WHERE url='/p' AND state='candidate'`)
	if err == nil {
		t.Fatal("promoting before retiring was accepted")
	}
	if !strings.Contains(err.Error(), "documents_one_published_per_url") {
		t.Errorf("error does not name the index that refused it: %v", err)
	}

	mustExec(t, db, `UPDATE documents SET state='superseded' WHERE url='/p' AND state='published'`)
	mustExec(t, db, `UPDATE documents SET state='published', published_at=UTC_TIMESTAMP(6)
		WHERE url='/p' AND state='candidate'`)
	mustExec(t, db, `DELETE FROM documents WHERE state='superseded'`)

	var live, hash string
	if err := db.QueryRow(`SELECT state, content_hash FROM documents WHERE url='/p'`).Scan(&live, &hash); err != nil {
		t.Fatal(err)
	}
	if live != "published" || hash != "h2" {
		t.Errorf("after publish: state=%q hash=%q, want published/h2", live, hash)
	}
}

// Deleting a page must take its chunks and their vectors with it, so the
// corpus cannot hold a chunk whose page is gone.
func TestLiveDeletingAPageRemovesItsChunks(t *testing.T) {
	db := open(t)
	if err := insertDoc(db, "/c", "h", "candidate"); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO chunks (document_id, ordinal, text, token_count)
		SELECT id, 0, 'text', 2 FROM documents WHERE url='/c'`)

	mustExec(t, db, `DELETE FROM documents WHERE url='/c'`)
	var chunks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&chunks); err != nil {
		t.Fatal(err)
	}
	if chunks != 0 {
		t.Errorf("%d chunks survived their page, want 0", chunks)
	}
}

// An address longer than the column allows must be a failed page, never a
// truncated one, because a truncated address is a different address. This
// depends on the server running in strict mode, so the test asserts the
// behaviour rather than trusting the configuration.
func TestLiveOverlongAddressIsRefused(t *testing.T) {
	db := open(t)
	err := insertDoc(db, "/"+strings.Repeat("a", 899), "h", "candidate")
	if err == nil {
		t.Fatal("an address longer than the column was accepted, so it was silently truncated")
	}
	if !strings.Contains(err.Error(), "Data too long") {
		t.Errorf("unexpected refusal: %v", err)
	}
}

// Two clients in different time zones must record the same instant, which is
// why no column defaults to a clock.
func TestLiveTimestampsAgreeAcrossTimeZones(t *testing.T) {
	db := open(t)
	mustExec(t, db, "SET time_zone = '+00:00'")
	mustExec(t, db, `INSERT INTO address_settings (url, included, updated_at) VALUES ('/a', TRUE, UTC_TIMESTAMP(6))`)
	mustExec(t, db, "SET time_zone = '+09:00'")
	mustExec(t, db, `INSERT INTO address_settings (url, included, updated_at) VALUES ('/b', TRUE, UTC_TIMESTAMP(6))`)

	var hours int
	if err := db.QueryRow(`SELECT ABS(TIMESTAMPDIFF(HOUR,
		(SELECT updated_at FROM address_settings WHERE url='/a'),
		(SELECT updated_at FROM address_settings WHERE url='/b')))`).Scan(&hours); err != nil {
		t.Fatal(err)
	}
	if hours != 0 {
		t.Errorf("two sessions recorded the same instant %d hours apart", hours)
	}
}

// The vector width appears in three places that no compiler links: the Go
// constant, the migration's column declaration, and siteconfig.toml. This is
// the assertion that keeps them in step.
//
// It reads SHOW CREATE TABLE rather than information_schema because
// information_schema.COLUMN_TYPE reports the type without its width, so a
// column of the wrong size is indistinguishable there.
func TestLiveVectorColumnMatchesTheConstant(t *testing.T) {
	db := open(t)
	var table, ddl string
	if err := db.QueryRow(`SHOW CREATE TABLE embeddings`).Scan(&table, &ddl); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("SVECTOR(%d)", config.MaxVectorDimensions)
	if !strings.Contains(ddl, want) {
		t.Errorf("embeddings.embedding is not declared %s:\n%s", want, ddl)
	}

	// Loading the real file is itself the assertion that ties it to the
	// constant: the validator refuses any width but MaxVectorDimensions, so a
	// successful load means the file agrees. Comparing the loaded value again
	// afterwards could never fail, which is why it is not done here.
	if _, err := config.Load("../../siteconfig.toml"); err != nil {
		t.Fatalf("siteconfig.toml disagrees with the column and the constant: %v", err)
	}
}

// One address may hold a live page while its candidate is rewritten, which is
// what lets a refresh run while the assistant keeps answering.
//
// This exercises the on-duplicate form rather than the store's method. The
// store looks the row up and then writes instead, because this dialect cannot
// return an identifier from an on-duplicate update. What is asserted here is
// the database rule the store depends on either way: the two states do not
// compete, so writing one cannot disturb the other.
func TestLiveUpsertLeavesThePublishedPageUntouched(t *testing.T) {
	db := open(t)
	mustExec(t, db, `INSERT INTO documents (url, markdown, content_hash, state, fetched_at)
		VALUES ('/u','live','published-hash','published',UTC_TIMESTAMP(6))`)

	for _, hash := range []string{"first-candidate", "second-candidate"} {
		mustExec(t, db, `INSERT INTO documents (url, markdown, content_hash, state, fetched_at)
			VALUES (?, 'draft', ?, 'candidate', UTC_TIMESTAMP(6))
			ON DUPLICATE KEY UPDATE content_hash = VALUES(content_hash), markdown = VALUES(markdown)`,
			"/u", hash)
	}

	var publishedHash string
	if err := db.QueryRow(
		`SELECT content_hash FROM documents WHERE url='/u' AND state='published'`).Scan(&publishedHash); err != nil {
		t.Fatal(err)
	}
	if publishedHash != "published-hash" {
		t.Errorf("the live page was modified by an upsert: content_hash = %q", publishedHash)
	}

	var candidates int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM documents WHERE url='/u' AND state='candidate'`).Scan(&candidates); err != nil {
		t.Fatal(err)
	}
	if candidates != 1 {
		t.Errorf("two upserts left %d candidates, want 1", candidates)
	}
}

// Publish must be all or nothing. A failure part-way through must leave the
// corpus exactly as it was, because a half-applied publish is a corpus that
// mixes two crawls and reports nothing.
//
// Data changes are transactional in MySQL even though schema changes are not,
// so this is a genuine rollback rather than a compensating update.
func TestLivePublishIsAllOrNothing(t *testing.T) {
	db := open(t)
	mustExec(t, db, `INSERT INTO documents (url, markdown, content_hash, state, fetched_at) VALUES
		('/a','old','a-live','published',UTC_TIMESTAMP(6)),
		('/a','new','a-draft','candidate',UTC_TIMESTAMP(6)),
		('/b','old','b-live','published',UTC_TIMESTAMP(6))`)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE documents SET state='superseded' WHERE url='/a' AND state='published'`); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE documents SET state='published' WHERE url='/a' AND state='candidate'`); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	// Something goes wrong after part of the work is done.
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query(`SELECT url, state, content_hash FROM documents ORDER BY url, state`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var url, state, hash string
		if err := rows.Scan(&url, &state, &hash); err != nil {
			t.Fatal(err)
		}
		got = append(got, url+" "+state+" "+hash)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"/a candidate a-draft", "/a published a-live", "/b published b-live"}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Errorf("a rolled-back publish changed the corpus:\n got  %v\n want %v", got, want)
	}
}

// Deleting a page must take its chunks and their vectors with it. The chunk
// cascade and the embedding cascade are separate foreign keys, so both are
// asserted rather than one standing in for the other.
func TestLiveDeletingAPageRemovesItsEmbeddings(t *testing.T) {
	db := open(t)
	if err := insertDoc(db, "/e", "h", "candidate"); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO chunks (document_id, ordinal, text, token_count)
		SELECT id, 0, 'text', 2 FROM documents WHERE url='/e'`)

	// The vector is written as a literal because a computed value cannot be
	// assigned to this column type. That is the same route the ingest path
	// takes, so the test exercises the real shape.
	vector := "[" + strings.Repeat("0.1,", config.MaxVectorDimensions-1) + "0.1]"
	mustExec(t, db, `INSERT INTO embeddings (chunk_id, embedding)
		SELECT id, '`+vector+`' FROM chunks`)

	var before int
	if err := db.QueryRow(`SELECT COUNT(*) FROM embeddings`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 1 {
		t.Fatalf("setup stored %d embeddings, want 1", before)
	}

	mustExec(t, db, `DELETE FROM documents WHERE url='/e'`)

	var chunks, embeddings int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&chunks); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM embeddings`).Scan(&embeddings); err != nil {
		t.Fatal(err)
	}
	if chunks != 0 || embeddings != 0 {
		t.Errorf("after deleting the page: %d chunks and %d embeddings survived, want 0 and 0", chunks, embeddings)
	}
}

// seedOneEmbedding stores one page, one chunk and one vector, which is the
// least a similarity query can be run against.
func seedOneEmbedding(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := insertDoc(db, "/vector", "h", "published"); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO chunks (document_id, ordinal, text, token_count)
		SELECT id, 0, 'text', 2 FROM documents WHERE url='/vector'`)
	vector := "[" + strings.Repeat("0.1,", config.MaxVectorDimensions-1) + "0.1]"
	mustExec(t, db, `INSERT INTO embeddings (chunk_id, embedding)
		SELECT id, '`+vector+`' FROM chunks`)
}

// queryVector is the text form of a vector, which is the only form anything
// here accepts. Its contents do not matter; only that it is the right width.
func queryVector() string {
	return "[" + strings.Repeat("0.2,", config.MaxVectorDimensions-1) + "0.2]"
}

// A session variable belongs to the one connection it was set on. Go's
// database/sql is a pool that hands out an arbitrary free connection per call,
// so a search that sets the query vector in one statement and reads it in the
// next can have the two land on different connections.
//
// This is the property that makes the search path unsafe on the pool, asserted
// directly rather than by trying to provoke the pool into doing it, which
// would be a race and would pass most of the time.
func TestLiveSessionVariablesDoNotCrossConnections(t *testing.T) {
	db := open(t)
	db.SetMaxOpenConns(2)
	ctx := context.Background()

	writer, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	reader, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if _, err := writer.ExecContext(ctx, "SET @probe = 'set on one connection'"); err != nil {
		t.Fatal(err)
	}

	var here sql.NullString
	if err := writer.QueryRowContext(ctx, "SELECT @probe").Scan(&here); err != nil {
		t.Fatal(err)
	}
	if !here.Valid {
		t.Fatal("the variable was not readable on the connection that set it, so this test proves nothing")
	}

	var there sql.NullString
	if err := reader.QueryRowContext(ctx, "SELECT @probe").Scan(&there); err != nil {
		t.Fatal(err)
	}
	if there.Valid {
		t.Errorf("a session variable set on one connection read back as %q on another; "+
			"the search path could stop needing a pinned connection", there.String)
	}
}

// The search path, run the way it must be run: both statements on one pinned
// connection. This is the form the architecture constraint prescribes, so it
// is asserted rather than described.
func TestLiveTheQueryVectorRoundTripsOnOnePinnedConnection(t *testing.T) {
	db := open(t)
	ctx := context.Background()
	seedOneEmbedding(t, db)

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SET @q = ?", queryVector()); err != nil {
		t.Fatalf("setting the query vector into a session variable failed: %v", err)
	}
	var similarity float64
	err = conn.QueryRowContext(ctx,
		`SELECT 1 - COSINE_DISTANCE(embedding, SVECTOR::FROM_STRING(@q)) FROM embeddings LIMIT 1`).
		Scan(&similarity)
	if err != nil {
		t.Fatalf("the prescribed search shape failed on a pinned connection: %v", err)
	}
	if similarity < -1 || similarity > 1 {
		t.Errorf("similarity came back as %v, which is outside the range a cosine distance can produce", similarity)
	}
}

// Whether the session variable is needed at all comes down to one question
// nobody has asked the server: does the vector constructor accept a bound
// parameter? A bound parameter is rejected as the distance function's own
// argument, measured, and the reason given is that the type is inferred before
// the parameter is known. That reason predicts this fails too.
//
// So this test asserts the limitation, and fails when the limitation lifts.
// Failing here is good news, not a regression: it means the search path can be
// one pooled statement with nothing pinned, and two documents are now wrong.
func TestLiveABoundParameterIsStillRejectedByFromString(t *testing.T) {
	db := open(t)
	seedOneEmbedding(t, db)

	var similarity float64
	err := db.QueryRow(
		`SELECT 1 - COSINE_DISTANCE(embedding, SVECTOR::FROM_STRING(?)) FROM embeddings LIMIT 1`,
		queryVector()).Scan(&similarity)
	if err == nil {
		t.Fatalf("a bound parameter through SVECTOR::FROM_STRING was ACCEPTED, returning %v.\n"+
			"This is good news and this test is the notification. The query vector no longer needs a\n"+
			"session variable, so search needs no pinned connection. Update the architecture constraint\n"+
			"in CLAUDE.md, the search path in PORTING.md, and replace this test with one asserting the\n"+
			"parameter form works.", similarity)
	}
	t.Logf("still rejected, as the recorded mechanism predicts: %v", err)
}

// No index may be a leftmost prefix of another, because the optimiser can
// never choose it instead of the longer one, while the server still maintains
// it on every insert and delete.
//
// This reads the catalogue rather than the file, so it also catches an index
// added to a database by hand and one added by a later migration.
//
// A unique index is exempt. Its columns may be a prefix of a longer index and
// it is still doing work no other index does, because it enforces a rule
// rather than only serving a lookup.
func TestLiveNoIndexIsRedundant(t *testing.T) {
	db := open(t)
	rows, err := db.Query(`
SELECT table_name, index_name, non_unique, GROUP_CONCAT(column_name ORDER BY seq_in_index)
  FROM information_schema.statistics
 WHERE table_schema = DATABASE()
 GROUP BY table_name, index_name, non_unique`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type index struct {
		name    string
		unique  bool
		columns string
	}
	byTable := map[string][]index{}
	for rows.Next() {
		var table, name, columns string
		var nonUnique int
		if err := rows.Scan(&table, &name, &nonUnique, &columns); err != nil {
			t.Fatal(err)
		}
		byTable[table] = append(byTable[table], index{name, nonUnique == 0, columns})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(byTable) == 0 {
		t.Fatal("no indexes were found at all, so this test checked nothing")
	}

	for table, indexes := range byTable {
		for _, a := range indexes {
			if a.unique {
				continue
			}
			for _, b := range indexes {
				if a.name == b.name || !strings.HasPrefix(b.columns+",", a.columns+",") {
					continue
				}
				t.Errorf("%s.%s (%s) is a leftmost prefix of %s (%s), so it can never be chosen "+
					"and is maintained on every write for nothing",
					table, a.name, a.columns, b.name, b.columns)
			}
		}
	}
}
