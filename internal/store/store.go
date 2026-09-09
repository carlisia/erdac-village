// Package store is the only path between this system and its database.
//
// One concrete type carries every method, and no package exports a wide
// interface over it. Each consumer declares the two to four methods it calls,
// beside the code that calls them, so a change made for one caller does not
// churn every other caller and its test double.
//
// Two rules hold at this boundary. No transaction handle crosses it: publish
// is a single method that is internally one transaction, with the
// retire-before-promote ordering sealed inside, because that ordering is a
// rule a caller could get wrong and the database would then refuse. Nothing
// database-shaped crosses it either: no nullable-column wrappers, no driver
// values, no result sets. Ordinary Go types and UTC times, with a pointer only
// where the absence of a value means something.
//
// Failures that a caller must distinguish are sentinel errors, matched by
// identity rather than by message, so the HTTP layer can map one to a status
// code without knowing which database is underneath.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/carlisia/erdac-village/internal/config"
	"github.com/carlisia/erdac-village/internal/migrations"
	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

// The connection pool's limits. Go's defaults are an unlimited number of
// connections that live forever, and both defaults are wrong here.
//
// Unlimited is wrong because the model functions hold a server thread for the
// whole outbound HTTP call, one row at a time. Under concurrent load an
// unbounded pool opens a connection per request, each parking a thread on a
// network call, until the server's own connection limit is reached and every
// caller is refused at once. A bounded pool makes requests wait for a
// connection instead, which is the same delay without the cliff.
//
// Living forever is wrong because the server closes an idle connection on its
// own timeout without telling the pool, so a handle held past that point is
// dead and the query using it fails on a connection that looked fine. Retiring
// connections before the server does removes that class of failure.
//
// These three are a judgement about the server rather than a fact about this
// code. They are deliberately well under a default server's connection limit,
// and the lifetime is well under a default idle timeout.
const (
	maxOpenConns    = 16
	maxIdleConns    = 8
	connMaxLifetime = 30 * time.Minute
)

// DB is a handle on the database. The package is named for the noun so that
// the reference at a call site reads as a handle rather than as a repeated
// word.
type DB struct {
	db *sqlx.DB
}

// Open connects to the database, forcing the four connection settings the rest
// of this package depends on.
//
// They are forced rather than required of the caller because each one is
// silent when wrong. Without parseTime a DATETIME arrives as bytes; without a
// UTC location it is parsed in the machine's zone; without the session zone
// pinned the server's own time functions evaluate somewhere else again; and
// without multiple statements per call a migration file cannot be applied at
// all.
func Open(ctx context.Context, dsn string) (*DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse the connection string: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = true
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"

	handle, err := sqlx.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open the database: %w", err)
	}
	handle.SetMaxOpenConns(maxOpenConns)
	handle.SetMaxIdleConns(maxIdleConns)
	handle.SetConnMaxLifetime(connMaxLifetime)
	if err := handle.PingContext(ctx); err != nil {
		// The ping failed, so this handle is being abandoned and its close
		// error would say nothing a caller could act on. Discarded rather than
		// joined, because reporting two failures for one event reads as two.
		_ = handle.Close()
		return nil, fmt.Errorf("reach the database: %w", err)
	}
	return New(handle), nil
}

// New wraps an already-open handle. Tests use it to supply a mock driver; Open
// is the only thing that should build a handle in production.
func New(handle *sqlx.DB) *DB {
	return &DB{db: handle}
}

// Close releases the connection pool.
func (d *DB) Close() error { return d.db.Close() }

// Migrate brings the database up to the current schema version and reports
// which migrations ran.
//
// It checks that the vector extension is present before applying anything,
// because the schema declares a vector column and migrations cannot roll back.
// Applying without the extension leaves the earlier tables created and reports
// an unrecognised type, which is a state a person then has to diagnose.
func (d *DB) Migrate(ctx context.Context) ([]migrations.Migration, error) {
	if err := d.checkVectorExtension(ctx); err != nil {
		return nil, err
	}
	return migrations.Apply(ctx, d.db.DB)
}

// checkVectorExtension asks the server for its widest vector, which is the one
// question that both proves the extension is loaded and reports whether an
// embedding would fit.
//
// A server that does not have the extension does not know the function, and
// says so with a specific error number. Only that number means the extension is
// absent. Reporting every failure as an absent extension sends an operator to
// install something that is already installed, when the real fault was a
// dropped connection, a denied permission, or an expired context.
//
// Unmeasured: that this is the number the server returns for an unknown
// function is upstream behaviour, not yet observed against an instance with the
// extension uninstalled. If it turns out to be another number, this check stops
// recognising the case and the error passes through unchanged, which is the
// safe direction to be wrong in.
func (d *DB) checkVectorExtension(ctx context.Context) error {
	var maximum int
	err := d.db.QueryRowContext(ctx, `SELECT VECTOR_MAX_DIMENSION()`).Scan(&maximum)
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == errFunctionNotFound {
			return fmt.Errorf("%w: %v", ErrVectorExtensionMissing, err)
		}
		return fmt.Errorf("ask the server for its maximum vector width: %w", err)
	}
	if maximum < config.MaxVectorDimensions {
		return fmt.Errorf("%w: server allows %d, embeddings are %d wide",
			ErrVectorTooNarrow, maximum, config.MaxVectorDimensions)
	}
	return nil
}
