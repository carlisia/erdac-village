package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// PublishResult reports what one publish did, so an administrator can tell
// whether it did what they expected.
type PublishResult struct {
	// Promoted is the number of candidates that went live.
	Promoted int
	// Retired is the number of pages they replaced, all of which were deleted.
	Retired int
}

// Publish promotes every included candidate at once and deletes the pages they
// replaced.
//
// It is one transaction, and the ordering inside it is load-bearing rather
// than stylistic. Retiring the live pages before promoting the candidates is
// what keeps the promotion from colliding with the live page for the same
// address, which is the failure the original system shipped once. The
// transaction is sealed inside this method for that reason: a caller handed a
// transaction handle can get the ordering wrong, and this one cannot.
//
// Either every included candidate goes live or none does. A publish that fails
// part-way leaves the corpus exactly as it was, because a half-applied publish
// is a corpus that mixes two crawls and reports nothing about it.
//
// It is refused while a fetch is running, because the candidate set would be
// half-written.
func (d *DB) Publish(ctx context.Context, at time.Time) (PublishResult, error) {
	var result PublishResult
	at = at.UTC()

	// The isolation level is stated rather than inherited. Locking the
	// candidate set below is what keeps a candidate written mid-publish from
	// being silently left behind, and that only holds where a range lock also
	// blocks an insert into the range it covers. Under the weaker level that
	// many servers are configured with, the same statement takes no such lock,
	// the publish misses the new candidate, and it reports success with a
	// promoted count one lower than the administrator expects. Asking for the
	// level costs nothing and removes the dependency on how a server was set up.
	tx, err := d.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return result, fmt.Errorf("publish: %w", translate(err))
	}
	defer tx.Rollback()

	// Read the system state under a lock first, so a fetch cannot start
	// between the check and the work. Checking without the lock would leave a
	// window in which both run.
	lock, err := readFetchLock(ctx, tx)
	if errors.Is(err, ErrNoSystemState) {
		return result, fmt.Errorf("publish: %w", ErrNoSystemState)
	}
	if err != nil {
		return result, fmt.Errorf("publish: read the system state: %w", translate(err))
	}
	// The age is measured against the server's clock rather than against the
	// publish timestamp. They are different things: a caller stamping a
	// publish with any time far enough ahead of the lock would otherwise make
	// a running fetch read as abandoned and promote its half-written pages.
	if lock.held() {
		return result, fmt.Errorf("publish: %w", ErrPublishDuringFetch)
	}
	// An expired lock is not the same as no lock. It is the trace of a fetch
	// that stopped without releasing, so the candidates in the database are
	// half of one crawl. Expiring the lock lets a new fetch start, which is
	// what keeps a crashed run from locking the system out; it does not make
	// what that run wrote safe to promote. The recovery is a fetch that runs
	// to completion, which replaces those candidates and releases the lock.
	if lock.abandoned() {
		return result, fmt.Errorf("publish: last held by %q since %s: %w",
			lock.owner.String, lock.since(), ErrAbandonedFetch)
	}
	// A takeover happened and no force fetch has completed since. The waiting
	// pages may be two runs' worth, and nothing else in the row would say so.
	if lock.takenOverAt.Valid {
		return result, fmt.Errorf("publish: takeover recorded at %s: %w",
			lock.takenOverAt.Time.UTC().Format(time.RFC3339), ErrTakeoverPending)
	}

	// The candidate set, locked, so nothing is added to it while this runs.
	// An address with no recorded judgement is included, which is why the
	// judgement is joined rather than required.
	//
	// A candidate on an excluded address is simply not in this set. It is not
	// deleted either: deleting it would destroy a page nobody has reviewed,
	// which is the one thing an administrator cannot undo by changing their
	// mind about the address.
	type candidate struct {
		ID  int64  `db:"id"`
		URL string `db:"url"`
	}
	var promote []candidate
	err = tx.SelectContext(ctx, &promote, `
SELECT d.id, d.url
  FROM documents d
  LEFT JOIN address_settings s ON s.url = d.url
 WHERE d.state = 'candidate' AND COALESCE(s.included, TRUE)
 FOR UPDATE`)
	if err != nil {
		return result, fmt.Errorf("publish: read the candidate set: %w", translate(err))
	}

	if len(promote) == 0 {
		if err := stampPublished(ctx, tx, at); err != nil {
			return result, err
		}
		if err := tx.Commit(); err != nil {
			return result, fmt.Errorf("publish: %w", translate(err))
		}
		return result, nil
	}

	urls := make([]string, len(promote))
	ids := make([]int64, len(promote))
	for i, c := range promote {
		urls[i] = c.URL
		ids[i] = c.ID
	}

	// The pages about to be replaced, identified before anything moves so the
	// deletion at the end names rows rather than a state. Deleting by state
	// would also remove a stray superseded row this publish did not create,
	// and this method should only undo what it did.
	var retire []int64
	query, args, err := sqlx.In(
		`SELECT id FROM documents WHERE state = 'published' AND url IN (?) FOR UPDATE`, urls)
	if err != nil {
		return result, fmt.Errorf("publish: build the retirement set: %w", err)
	}
	if err := tx.SelectContext(ctx, &retire, tx.Rebind(query), args...); err != nil {
		return result, fmt.Errorf("publish: read the retirement set: %w", translate(err))
	}

	// Retire, then promote. This order is the specification, not an
	// implementation detail: promoting first collides with the live page for
	// the same address and the database refuses it.
	if len(retire) > 0 {
		if _, err := execOver(ctx, tx, "retire the replaced pages",
			`UPDATE documents SET state = 'superseded' WHERE id IN (?)`, retire); err != nil {
			return result, err
		}
	}

	result.Promoted, err = execOver(ctx, tx, "promote the candidates",
		`UPDATE documents SET state = 'published', published_at = ? WHERE id IN (?)`, ids, at)
	if err != nil {
		return result, err
	}

	// The retired pages are deleted, taking their chunks and vectors with
	// them, so storage does not grow without limit and no superseded page can
	// be retrieved.
	if len(retire) > 0 {
		result.Retired, err = execOver(ctx, tx, "delete the retired pages",
			`DELETE FROM documents WHERE id IN (?)`, retire)
		if err != nil {
			return result, err
		}
	}

	if err := stampPublished(ctx, tx, at); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("publish: %w", translate(err))
	}
	return result, nil
}

// stampPublished records when the corpus last changed. It runs even when
// nothing was promoted, because "published, and there was nothing to publish"
// is what tells an administrator that nothing changed rather than that nothing
// was reviewed.
func stampPublished(ctx context.Context, tx *sqlx.Tx, at time.Time) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE system_state SET last_published_at = ? WHERE id = 1`, at); err != nil {
		return fmt.Errorf("publish: stamp the publish time: %w", translate(err))
	}
	return nil
}

// execOver runs a statement over a list of identifiers and reports how many
// rows it changed.
//
// Expanding the list, rebinding the placeholders and reading the row count are
// the same three steps at every call site inside publish, and the row count is
// what the result is built from. Written out at each site they were three
// chances to drop one, which is a fault that reports a smaller number rather
// than an error.
//
// Extra arguments come before the list because the list is always last in
// these statements.
func execOver(ctx context.Context, tx *sqlx.Tx, what, statement string, ids []int64, args ...any) (int, error) {
	query, expanded, err := sqlx.In(statement, append(args, ids)...)
	if err != nil {
		return 0, fmt.Errorf("publish: build the statement to %s: %w", what, err)
	}
	res, err := tx.ExecContext(ctx, tx.Rebind(query), expanded...)
	if err != nil {
		return 0, fmt.Errorf("publish: %s: %w", what, translate(err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("publish: count the rows changed to %s: %w", what, err)
	}
	return int(n), nil
}
