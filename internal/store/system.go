package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// FetchLockTTL is the age at which a held fetch lock is treated as abandoned
// rather than held, so a crashed run does not lock the system out permanently.
//
// The value is a judgement, not a measurement: no full crawl has been timed
// yet. It must stay comfortably above the longest a healthy fetch can take,
// because expiring a live lock lets two crawls interleave into one candidate
// set, which is the failure the lock exists to prevent.
const FetchLockTTL = 30 * time.Minute

// SystemState reads the single row that holds what is true of the running
// system.
func (d *DB) SystemState(ctx context.Context) (SystemState, error) {
	var (
		s             SystemState
		lock          sql.NullString
		started       sql.NullTime
		takenOver     sql.NullTime
		fetched       sql.NullTime
		lastPublished sql.NullTime
	)
	err := d.db.QueryRowContext(ctx, `
SELECT halted, session_epoch, fetch_lock, fetch_started_at, fetch_taken_over_at,
       last_fetched_at, last_published_at
  FROM system_state WHERE id = 1`).
		Scan(&s.Halted, &s.SessionEpoch, &lock, &started, &takenOver, &fetched, &lastPublished)
	if errors.Is(err, sql.ErrNoRows) {
		return s, fmt.Errorf("read the system state: %w", ErrNoSystemState)
	}
	if err != nil {
		return s, fmt.Errorf("read the system state: %w", translate(err))
	}
	s.FetchLock = lock.String
	s.FetchStartedAt = timeFromColumn(started)
	s.FetchTakenOverAt = timeFromColumn(takenOver)
	s.LastFetchedAt = timeFromColumn(fetched)
	s.LastPublishedAt = timeFromColumn(lastPublished)
	return s, nil
}

// SetHalted switches the assistant off or back on. Halting stops it answering
// without taking the site down, so it is a flag rather than a deployment
// change. Every request checks it; the disabled input in the browser is
// presentation only.
//
// The row's presence is confirmed after the write rather than read off the
// write's row count, because the driver counts rows changed rather than rows
// matched: setting the flag to the value it already holds reports zero, the
// same as a row that was never there. Without the check an update that matched
// nothing returns success while the flag stays wherever it was, and for the
// switch that stops the assistant answering that is the one failure that has
// to be loud.
func (d *DB) SetHalted(ctx context.Context, halted bool) error {
	if _, err := d.db.ExecContext(ctx,
		`UPDATE system_state SET halted = ? WHERE id = 1`, halted); err != nil {
		return fmt.Errorf("set the halted flag: %w", translate(err))
	}
	var present int
	err := d.db.QueryRowContext(ctx, `SELECT 1 FROM system_state WHERE id = 1`).Scan(&present)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("set the halted flag: %w", ErrNoSystemState)
	}
	if err != nil {
		return fmt.Errorf("set the halted flag: %w", translate(err))
	}
	return nil
}

// ClearConversations moves the epoch that open browser tabs compare against,
// and returns its new value. Any change to it, in either direction, makes a
// tab discard its conversation, so a visitor cannot continue from a state an
// administrator no longer wants.
func (d *DB) ClearConversations(ctx context.Context) (int, error) {
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("clear conversations: %w", translate(err))
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE system_state SET session_epoch = session_epoch + 1 WHERE id = 1`); err != nil {
		return 0, fmt.Errorf("clear conversations: %w", translate(err))
	}
	var epoch int
	if err := tx.QueryRowContext(ctx,
		`SELECT session_epoch FROM system_state WHERE id = 1`).Scan(&epoch); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("clear conversations: %w", ErrNoSystemState)
		}
		return 0, fmt.Errorf("clear conversations: %w", translate(err))
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("clear conversations: %w", translate(err))
	}
	return epoch, nil
}

// AcquireFetchLock takes the fetch lock for one run, so a second fetch cannot
// start and interleave its pages into this one's candidate set.
//
// The lock is advisory and the database does not enforce it. Publish refuses
// while it is held, but UpsertCandidate, ReplaceChunks and StoreEmbeddings do
// not check it at all, so the property that a publish never promotes a
// half-written candidate set holds only because every candidate writer calls
// this first. A writer that does not gets no error and leaves a corpus mixing
// two crawls. Anything that writes a candidate must take this lock.
//
// A lock older than FetchLockTTL is treated as abandoned rather than held and
// is taken over, so a crashed run does not lock the system out permanently.
//
// Both the age and the moment it is measured against come from the server's
// clock, and the caller supplies no time at all. Writing the start time from
// one clock and judging its age against another means a machine whose clock
// runs behind sees its own fresh lock as already abandoned, and two fetches
// then run at once. One clock is the only arrangement in which the age means
// anything, and the server's is the one every client shares.
//
// The check and the write happen under a row lock in one transaction. Reading
// the state and then writing it would leave a window in which two runs both
// see the lock free.
func (d *DB) AcquireFetchLock(ctx context.Context, owner string) error {
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("take the fetch lock: %w", translate(err))
	}
	defer tx.Rollback()

	lock, err := readFetchLock(ctx, tx)
	if err != nil {
		return fmt.Errorf("take the fetch lock: %w", translate(err))
	}
	if lock.held() {
		return fmt.Errorf("take the fetch lock, held by %q since %s: %w",
			lock.owner.String, lock.since(), ErrFetchInProgress)
	}
	// Taking over an abandoned lock is recorded, because the crashed run's
	// pages are still waiting for review and nothing else would say so once
	// this run finishes cleanly. Publish refuses while the record stands.
	statement := `UPDATE system_state SET fetch_lock = ?, fetch_started_at = UTC_TIMESTAMP(6) WHERE id = 1`
	if lock.abandoned() {
		statement = `UPDATE system_state
		                SET fetch_lock = ?, fetch_started_at = UTC_TIMESTAMP(6),
		                    fetch_taken_over_at = COALESCE(fetch_taken_over_at, UTC_TIMESTAMP(6))
		              WHERE id = 1`
	}
	if _, err := tx.ExecContext(ctx, statement, owner); err != nil {
		return fmt.Errorf("take the fetch lock: %w", translate(err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("take the fetch lock: %w", translate(err))
	}
	return nil
}

// RenewFetchLock moves the lock's start time to now, so a crawl that is still
// running is never mistaken for one that crashed. A crawl calls this on an
// interval well inside FetchLockTTL. It refuses when the lock is no longer
// this run's, which tells the crawl to stop rather than write another page.
func (d *DB) RenewFetchLock(ctx context.Context, owner string) error {
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("renew the fetch lock: %w", translate(err))
	}
	defer tx.Rollback()

	lock, err := readFetchLock(ctx, tx)
	if err != nil {
		return fmt.Errorf("renew the fetch lock: %w", translate(err))
	}
	if lock.owner.String != owner {
		return fmt.Errorf("renew the fetch lock, now held by %q: %w", lock.owner.String, ErrFetchLockLost)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE system_state SET fetch_started_at = UTC_TIMESTAMP(6) WHERE id = 1`); err != nil {
		return fmt.Errorf("renew the fetch lock: %w", translate(err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("renew the fetch lock: %w", translate(err))
	}
	return nil
}

// ReleaseFetchLock gives the lock up. A nil time releases without recording a
// fetch, which is what a run that failed must do: recording a fetch time it
// did not finish would make a failed run look like a completed one.
//
// full says the run visited every address rather than skipping ones that
// looked unchanged. A completed full run is the one condition under which
// every waiting candidate is known to come from a single run, so it is the
// only thing that clears a recorded takeover and lets publish proceed again.
//
// It refuses when the lock is no longer this run's, which happens when the run
// took longer than FetchLockTTL and another fetch took the lock over. Clearing
// it then would release a lock that a live run is relying on.
func (d *DB) ReleaseFetchLock(ctx context.Context, owner string, fetchedAt *time.Time, full bool) error {
	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("release the fetch lock: %w", translate(err))
	}
	defer tx.Rollback()

	var lock sql.NullString
	err = tx.QueryRowContext(ctx,
		`SELECT fetch_lock FROM system_state WHERE id = 1 FOR UPDATE`).Scan(&lock)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("release the fetch lock: %w", ErrNoSystemState)
	}
	if err != nil {
		return fmt.Errorf("release the fetch lock: %w", translate(err))
	}
	// The two ways to lose a lock have different remedies. A lock nobody holds
	// means this run released twice, which is a fault in the caller; a lock
	// another run holds means this run overran the expiry. Printing an empty
	// name for the first would read as the second with the name missing.
	if lock.String != owner {
		if lock.String == "" {
			return fmt.Errorf("release the fetch lock, which nobody holds: %w", ErrFetchLockLost)
		}
		return fmt.Errorf("release the fetch lock, now held by %q: %w", lock.String, ErrFetchLockLost)
	}

	switch {
	case fetchedAt == nil:
		_, err = tx.ExecContext(ctx,
			`UPDATE system_state SET fetch_lock = NULL, fetch_started_at = NULL WHERE id = 1`)
	case full:
		_, err = tx.ExecContext(ctx,
			`UPDATE system_state
			    SET fetch_lock = NULL, fetch_started_at = NULL, fetch_taken_over_at = NULL,
			        last_fetched_at = ?
			  WHERE id = 1`, fetchedAt.UTC())
	default:
		_, err = tx.ExecContext(ctx,
			`UPDATE system_state SET fetch_lock = NULL, fetch_started_at = NULL, last_fetched_at = ?
			 WHERE id = 1`, fetchedAt.UTC())
	}
	if err != nil {
		return fmt.Errorf("release the fetch lock: %w", translate(err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("release the fetch lock: %w", translate(err))
	}
	return nil
}

// readFetchLock reads the lock under a row lock, together with the server's
// own clock, so the age of the lock is measured against the same clock that
// wrote its start time.
//
// The three values come from one statement rather than three, which is what
// makes them describe one moment. Reading the clock separately would let the
// lock be taken between the two reads.
func readFetchLock(ctx context.Context, tx *sqlx.Tx) (fetchLock, error) {
	var f fetchLock
	err := tx.QueryRowContext(ctx,
		`SELECT fetch_lock, fetch_started_at, fetch_taken_over_at, UTC_TIMESTAMP(6)
		   FROM system_state WHERE id = 1 FOR UPDATE`).
		Scan(&f.owner, &f.startedAt, &f.takenOverAt, &f.serverNow)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNoSystemState
	}
	f.serverNow = f.serverNow.UTC()
	return f, err
}

// fetchLock is one reading of the lock: who holds it, since when, and what the
// server's clock said at that instant. The three are only meaningful together,
// which is why they are read in one statement and carried as one value.
type fetchLock struct {
	owner       sql.NullString
	startedAt   sql.NullTime
	takenOverAt sql.NullTime
	serverNow   time.Time
}

// held reports whether the lock was live at the moment it was read. The moment
// is always the server's own clock, which is why it is part of the reading.
//
// A lock with no start time counts as live rather than as abandoned. It should
// not occur, because the two columns are always written together. If it ever
// does, refusing a second fetch is the safe reading: a spurious refusal is
// visible and recoverable, whereas two interleaved crawls produce a candidate
// set that looks healthy and is wrong.
func (f fetchLock) held() bool {
	return f.present() && !f.expired()
}

// present reports whether a lock was left in the row at all, whether or not it
// is still live. A lock that is present but expired is the trace of a run that
// stopped without releasing it.
func (f fetchLock) present() bool {
	return f.owner.Valid && f.owner.String != ""
}

// abandoned reports a lock left behind by a run that did not finish.
func (f fetchLock) abandoned() bool {
	return f.present() && f.expired()
}

func (f fetchLock) expired() bool {
	if !f.startedAt.Valid {
		return false
	}
	return f.serverNow.Sub(f.startedAt.Time.UTC()) >= FetchLockTTL
}

// since renders the start time for a refusal message. A lock with no start
// time is named as such rather than printed as the zero time, which reads as a
// date in the year one and sends a reader looking for a clock fault.
func (f fetchLock) since() string {
	if !f.startedAt.Valid {
		return "an unrecorded time"
	}
	return f.startedAt.Time.UTC().Format(time.RFC3339)
}
