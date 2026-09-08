// Package schema holds the live-tier tests for the database schema.
//
// These require a running server and are skipped when TEST_MYSQL_DSN is unset.
// They exist because a fake cannot prove any of what they assert: every rule
// below is enforced by the database, not by Go, so a test that substituted the
// database would only be asserting this file's own beliefs about MySQL.
//
// Every test name begins with Live so the check script can select them.
package schema

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/carlisia/erdac-village/internal/config"
	_ "github.com/go-sql-driver/mysql"
)

const migration = "../../migrations/0001_schema.sql"

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
	b.WriteString("village_live_")
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

func apply(t *testing.T, db *sql.DB) {
	t.Helper()
	raw, err := os.ReadFile(migration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Exec(string(raw)); err != nil {
		t.Fatalf("apply migration: %v", err)
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

// An upsert of a candidate must not disturb the live page for that address.
// This is what lets a refresh run while the assistant keeps answering.
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
