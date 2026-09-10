// The fast tier for the migration set. No database.
//
// What it can assert is what the embedded files say about themselves: that
// they are all present, that their names parse, and that they are handed out
// in the order they must be applied. Whether applying them produces the right
// schema is a question only a server can answer, and the live tier asks it.
package migrations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// A migration is selected and ordered entirely by its file name, so a file the
// naming rule cannot parse must be an error rather than something skipped. A
// silently skipped migration is a database missing a table nothing reports.
func TestEveryEmbeddedFileParses(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatalf("the embedded migrations do not parse: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no migrations are embedded, so a database would be brought up empty")
	}
	for _, m := range all {
		if m.Version <= 0 {
			t.Errorf("%s has version %d, which cannot be ordered", m.Name, m.Version)
		}
		if strings.TrimSpace(m.SQL) == "" {
			t.Errorf("%s is empty", m.Name)
		}
	}
}

// They must come back in ascending order, and no two may share a version: a
// version is the key of the record that says what has been applied, so two
// files claiming one version means one of them is never recorded as run.
func TestVersionsAreOrderedAndUnique(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]string{}
	previous := 0
	for _, m := range all {
		if m.Version <= previous {
			t.Errorf("%s (version %d) came after version %d, so they would apply out of order",
				m.Name, m.Version, previous)
		}
		if other, ok := seen[m.Version]; ok {
			t.Errorf("%s and %s both claim version %d", other, m.Name, m.Version)
		}
		seen[m.Version] = m.Name
		previous = m.Version
	}
}

// Every migration records itself, because the migrator decides what still
// needs running by reading that record. A file that does not write its own row
// is applied again on every run.
func TestEveryMigrationRecordsItself(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if !strings.Contains(m.SQL, "schema_migrations") {
			t.Errorf("%s never writes to schema_migrations, so it would re-apply forever", m.Name)
		}
	}
}

// The recovery path for a failure part-way through is to run the file again,
// which only works if nothing in it assumes it starts from nothing.
//
// The count is taken over the statements alone. Comments in these files
// discuss the very constructs being counted, so counting the whole text would
// match prose and report a fault that is not there.
func TestEveryStatementIsSafeToRepeat(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		upper := strings.ToUpper(withoutComments(m.SQL))
		for statement, guard := range map[string]string{
			"CREATE TABLE": "CREATE TABLE IF NOT EXISTS",
			"INSERT INTO":  "ON DUPLICATE KEY UPDATE",
		} {
			if got, want := strings.Count(upper, guard), strings.Count(upper, statement); got != want {
				t.Errorf("%s has %d %s but only %d guarded by %s, so re-running it would fail",
					m.Name, want, statement, got, guard)
			}
		}
	}
}

// A failure that arrives while the version rows are streaming is reported by
// the iteration rather than by the query. It has to name the operation the
// same way a failure before the first row does, because the migrator's caller
// is a person reading a terminal, and "connection lost" alone says nothing
// about which of the migrator's three reads lost it.
func TestAFailureWhileReadingVersionsNamesTheOperation(t *testing.T) {
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open the mock driver: %v", err)
	}
	defer raw.Close()
	mock.ExpectQuery("information_schema.tables").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("SELECT version FROM schema_migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(1).
			RowError(0, errors.New("connection lost")))

	_, err = appliedVersions(context.Background(), raw)
	if err == nil {
		t.Fatal("a failure during iteration was reported as a successful read")
	}
	if !strings.Contains(err.Error(), "read applied versions") {
		t.Errorf("the message does not name the operation that hit it: %q", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// MySQL cannot make an ALTER conditional in the statement, so a file that
// alters a table must ask the catalogue and prepare the change only when it is
// needed. An ALTER written bare would fail the second time the file runs.
func TestEveryAlterIsPreparedConditionally(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		for _, line := range strings.Split(withoutComments(m.SQL), "\n") {
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "ALTER TABLE") {
				t.Errorf("%s alters a table outside a prepared statement, so re-running it would fail: %s",
					m.Name, strings.TrimSpace(line))
			}
		}
	}
}

// withoutComments drops the line comments from a migration. These files use no
// other comment form, and a block comment would need handling here before one
// could be written.
func withoutComments(sql string) string {
	var kept []string
	for _, line := range strings.Split(sql, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// The stripper is what the previous test rests on, so its own failure mode is
// asserted rather than assumed.
func TestWithoutCommentsKeepsStatements(t *testing.T) {
	got := withoutComments("-- INSERT INTO nothing\nINSERT INTO real VALUES (1); -- trailing\n")
	if strings.Contains(got, "nothing") {
		t.Error("a whole-line comment survived")
	}
	if !strings.Contains(got, "INSERT INTO real") {
		t.Error("a statement was dropped")
	}
}
