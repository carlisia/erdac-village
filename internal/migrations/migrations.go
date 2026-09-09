// Package migrations carries the numbered SQL files that bring a database from
// empty to current, and the runner that applies them.
//
// It lives under internal because Apply executes schema statements against any
// handle it is given and All exposes the embedded SQL, and neither is meant for
// a caller outside this module. At the module root both would have been part of
// this module's public compatibility surface.
//
// The files are embedded rather than read from disk, so a compiled binary can
// migrate a database with no repository present.
//
// Nothing here knows which driver it is talking to. The precondition that the
// vector extension is present is checked by the store before it calls Apply,
// because recognising the server's refusal means recognising a driver error
// number, and that knowledge lives at one boundary.
//
// Migrations are not transactional and cannot be: MySQL commits implicitly on
// every schema statement, so a failure part-way through leaves the earlier
// statements applied and a rollback does nothing. Idempotence is the only
// recovery mechanism.
//
// A file records itself as its last statement. A run that fails part-way
// therefore leaves no record, and the next run applies the whole file again
// over the tables it already created, which is why every statement in it has
// to be safe to repeat. Nothing re-applies a version that is already recorded.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
)

//go:embed *.sql
var files embed.FS

// Migration is one numbered file.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// All returns every embedded migration in version order.
//
// The version and the name both come from the file name, which is the only
// place they are written once the file is embedded. A file that does not begin
// with a number is a mistake rather than a migration, so it is an error rather
// than something silently skipped.
func All() ([]Migration, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	var out []Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".sql")
		digits, _, ok := strings.Cut(name, "_")
		if !ok {
			return nil, fmt.Errorf("migration %q is not named <version>_<name>.sql", e.Name())
		}
		version, err := strconv.Atoi(digits)
		if err != nil {
			return nil, fmt.Errorf("migration %q does not begin with a version number: %w", e.Name(), err)
		}
		body, err := fs.ReadFile(files, e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		out = append(out, Migration{Version: version, Name: name, SQL: string(body)})
	}
	slices.SortFunc(out, func(a, b Migration) int { return a.Version - b.Version })
	return out, nil
}

// Apply brings a database up to the current version and reports which
// migrations it ran.
//
// Every statement in every file is safe to repeat, so a version whose recorded
// row is absent is re-applied whatever state the database is actually in. That
// is what makes a run that failed part-way through recoverable by running it
// again: the file converges rather than assuming it starts from nothing.
//
// The connection must permit multiple statements per call, which store.Open
// arranges. A file holds many statements and splitting SQL on semicolons is
// wrong for any file containing one inside a string or a comment.
func Apply(ctx context.Context, db *sql.DB) ([]Migration, error) {
	pending, err := All()
	if err != nil {
		return nil, err
	}
	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return nil, err
	}

	var ran []Migration
	for _, m := range pending {
		if applied[m.Version] {
			continue
		}
		if _, err := db.ExecContext(ctx, m.SQL); err != nil {
			return ran, fmt.Errorf("migration %d (%s): %w", m.Version, m.Name, err)
		}
		ran = append(ran, m)
	}
	return ran, nil
}

// appliedVersions reads the version table, treating its absence as an empty
// database rather than as a failure. On a database that has never been
// migrated the table does not exist yet, and the first migration creates it.
func appliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	var exists int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema = DATABASE() AND table_name = 'schema_migrations'`).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("look for the version table: %w", err)
	}
	applied := map[int]bool{}
	if exists == 0 {
		return applied, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read applied versions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("read applied versions: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read applied versions: %w", err)
	}
	return applied, nil
}
