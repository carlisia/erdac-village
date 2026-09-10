package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// addressStatesQuery reads what is known about every address in one pass.
//
// The row source is the union of the addresses that hold a page and the
// addresses that hold a judgement, because either can exist without the other:
// an address can be excluded before it is ever fetched, and a fetched address
// usually carries no judgement at all.
//
// The two joins onto documents use the generated columns rather than a state
// comparison, so each one meets a unique index and can match at most one row.
// Joining on state instead would be correct and would also scan.
const addressStatesQuery = `
SELECT a.url,
       COALESCE(s.included, TRUE) AS included,
       p.id, p.title, p.content_hash, p.lastmod, p.fetched_at, p.published_at,
       c.id, c.title, c.content_hash, c.lastmod, c.fetched_at, c.published_at
  FROM (SELECT url FROM documents GROUP BY url
        UNION
        SELECT url FROM address_settings) AS a
  LEFT JOIN address_settings s ON s.url = a.url
  LEFT JOIN documents p ON p.url_published = a.url
  LEFT JOIN documents c ON c.url_candidate = a.url
 ORDER BY a.url`

// nullablePageSummary is one page as it arrives from a left join. Every column
// is nullable because the join may have matched nothing, which is how an
// address that holds no page in that state is reported.
//
// It exists so the two joined pages are read and built by the same code. They
// were previously eight parallel locals and two hand-written constructions,
// where a column added to one and forgotten in the other would compile and
// report the wrong thing for exactly one of the two states.
//
// A candidate's publish time is selected too, and is always NULL, so that both
// pages have the same shape. A page that has never been live saying so in the
// column meant for it is the honest form.
type nullablePageSummary struct {
	id          sql.NullInt64
	title       sql.NullString
	contentHash sql.NullString
	lastMod     sql.NullTime
	fetchedAt   sql.NullTime
	publishedAt sql.NullTime
}

// scanInto returns this page's destinations in the order the query selects
// them. Keeping the order here, next to the column list above, is what stops
// the two drifting apart.
func (n *nullablePageSummary) scanInto() []any {
	return []any{&n.id, &n.title, &n.contentHash, &n.lastMod, &n.fetchedAt, &n.publishedAt}
}

// summary returns nil when the join matched no page, which is how the caller
// is told the address holds none in that state.
func (n nullablePageSummary) summary() *PageSummary {
	if !n.id.Valid {
		return nil
	}
	return &PageSummary{
		ID:          n.id.Int64,
		Title:       n.title.String,
		ContentHash: n.contentHash.String,
		LastMod:     timeFromColumn(n.lastMod),
		FetchedAt:   n.fetchedAt.Time.UTC(),
		PublishedAt: timeFromColumn(n.publishedAt),
	}
}

// AddressStates reports the stored state of every address the system knows
// about, including addresses that carry a judgement but no page.
//
// This is what the ingest path reads to decide what to skip and what the
// review screen reads to list candidates, so it returns both the judgement and
// the two pages an address can hold rather than making either caller ask
// twice.
func (d *DB) AddressStates(ctx context.Context) ([]AddressState, error) {
	rows, err := d.db.QueryContext(ctx, addressStatesQuery)
	if err != nil {
		return nil, fmt.Errorf("read address states: %w", translate(err))
	}
	defer rows.Close()

	var out []AddressState
	for rows.Next() {
		var (
			a         AddressState
			published nullablePageSummary
			candidate nullablePageSummary
		)
		into := append([]any{&a.URL, &a.Included}, published.scanInto()...)
		into = append(into, candidate.scanInto()...)
		if err := rows.Scan(into...); err != nil {
			return nil, fmt.Errorf("read address states: %w", translate(err))
		}
		a.Published = published.summary()
		a.Candidate = candidate.summary()
		out = append(out, a)
	}
	// A failure that arrives mid-read is reported here rather than by the
	// query, and it is still a driver error: a deadlock broken while the rows
	// were streaming has to reach the caller as the retryable class, the same
	// as one broken before the first row.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read address states: %w", translate(err))
	}
	return out, nil
}

// StoreCandidate writes a fetched page as a candidate together with its chunks
// and their vectors, all in one transaction, and returns the page's identifier.
//
// A page is never stored published, so nothing this writes can answer a
// visitor before an administrator has approved it. Re-fetching an address
// overwrites its candidate rather than adding a second one, and leaves the
// published page for that address untouched, which is what lets a refresh run
// while the assistant keeps answering.
//
// One transaction, by decision. The original wrote the candidate, its chunks
// and its vectors as three calls and accepted the window between them, in
// which a process dying left a page whose chunks had no vectors and which
// search then could not find. Here the three land together or not at all.
//
// It refuses a run that no longer holds the fetch lock. The lock was advisory
// until this method: a run that overran the expiry and lost the lock to a
// newer fetch could keep writing, and the two runs' pages mixed into one set.
// The lock row is read under a shared lock inside the transaction, so a
// takeover cannot slip between the check and the write.
//
// The candidate is looked up and then written rather than inserted with an
// on-duplicate clause, because this dialect has no RETURNING and the
// on-duplicate form recovers the identifier by assigning it to itself, which
// MySQL skips when nothing changed. Chunk identifiers are collected one insert
// at a time, because consecutive identifiers for a batch are only guaranteed
// under one of the server's three allocation modes and the default is not it.
func (d *DB) StoreCandidate(ctx context.Context, owner string, p Page, chunks []Chunk, vectors [][]float32) (int64, error) {
	if err := checkAddress(p.URL); err != nil {
		return 0, fmt.Errorf("store candidate: %w", err)
	}
	if len(vectors) != len(chunks) {
		return 0, fmt.Errorf("store candidate %s: %d chunks but %d vectors; pairing them by position would attach the wrong meaning to the wrong text",
			p.URL, len(chunks), len(vectors))
	}
	for i, v := range vectors {
		if err := checkVector(v); err != nil {
			return 0, fmt.Errorf("store candidate %s, chunk %d: %w", p.URL, chunks[i].Ordinal, err)
		}
	}

	tx, err := d.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store candidate %s: %w", p.URL, translate(err))
	}
	defer tx.Rollback()

	if err := requireLock(ctx, tx, owner); err != nil {
		return 0, fmt.Errorf("store candidate %s: %w", p.URL, err)
	}

	var id int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM documents WHERE url_candidate = ? FOR UPDATE`, p.URL).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.ExecContext(ctx,
			`INSERT INTO documents (url, title, markdown, content_hash, lastmod, state, fetched_at)
			 VALUES (?, ?, ?, ?, ?, 'candidate', ?)`,
			p.URL, nullString(p.Title), p.Markdown, p.ContentHash,
			timeToColumn(p.LastMod), p.FetchedAt.UTC())
		if err != nil {
			return 0, fmt.Errorf("store candidate %s: %w", p.URL, translate(err))
		}
		if id, err = res.LastInsertId(); err != nil {
			return 0, fmt.Errorf("store candidate %s: %w", p.URL, err)
		}
	case err != nil:
		return 0, fmt.Errorf("store candidate %s: %w", p.URL, translate(err))
	default:
		_, err := tx.ExecContext(ctx,
			`UPDATE documents
			    SET title = ?, markdown = ?, content_hash = ?, lastmod = ?, fetched_at = ?
			  WHERE id = ?`,
			nullString(p.Title), p.Markdown, p.ContentHash,
			timeToColumn(p.LastMod), p.FetchedAt.UTC(), id)
		if err != nil {
			return 0, fmt.Errorf("store candidate %s: %w", p.URL, translate(err))
		}
	}

	// Replacing rather than appending keeps a re-fetch from leaving the previous
	// run's chunks behind. Deleting the chunks deletes their vectors too.
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, id); err != nil {
		return 0, fmt.Errorf("store candidate %s: replace chunks: %w", p.URL, translate(err))
	}
	for i, c := range chunks {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO chunks (document_id, ordinal, heading_path, text, token_count)
			 VALUES (?, ?, ?, ?, ?)`,
			id, c.Ordinal, nullString(c.HeadingPath), c.Text, c.TokenCount)
		if err != nil {
			return 0, fmt.Errorf("store candidate %s, chunk %d: %w", p.URL, c.Ordinal, translate(err))
		}
		chunkID, err := res.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("store candidate %s, chunk %d: %w", p.URL, c.Ordinal, err)
		}
		// The vector is concatenated as a literal because nothing casts a
		// computed vector into the fixed-width column type. See formatVector
		// for why that is safe.
		statement := `INSERT INTO embeddings (chunk_id, embedding) VALUES (?, '` +
			formatVector(vectors[i]) + `')`
		if _, err := tx.ExecContext(ctx, statement, chunkID); err != nil {
			return 0, fmt.Errorf("store candidate %s, chunk %d: embedding: %w", p.URL, c.Ordinal, translate(err))
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store candidate %s: %w", p.URL, translate(err))
	}
	return id, nil
}

// requireLock refuses a caller that does not hold the fetch lock, under a
// shared lock on the row so the answer cannot change before the transaction
// commits. A takeover has to wait for this transaction, and this transaction
// has to wait for a takeover in progress.
func requireLock(ctx context.Context, tx *sqlx.Tx, owner string) error {
	var lock sql.NullString
	err := tx.QueryRowContext(ctx,
		`SELECT fetch_lock FROM system_state WHERE id = 1 FOR SHARE`).Scan(&lock)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoSystemState
	}
	if err != nil {
		return translate(err)
	}
	if lock.String != owner {
		return fmt.Errorf("the lock is held by %q: %w", lock.String, ErrFetchLockLost)
	}
	return nil
}

// SetAddressIncluded records an administrator's standing judgement about an
// address.
//
// The judgement is stored against the address and never against a page, which
// is what makes it sticky across refreshes: a newly fetched candidate inherits
// it because there is nothing on the page for an upsert to carry forward.
//
// Excluding an address takes effect at once rather than at the next publish.
// The corpus is defined as the chunks of published pages whose address is
// included, so a published page on a newly excluded address stops being
// searched the moment this returns, without anything being deleted.
func (d *DB) SetAddressIncluded(ctx context.Context, url string, included bool, at time.Time) error {
	if err := checkAddress(url); err != nil {
		return fmt.Errorf("set judgement: %w", err)
	}
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO address_settings (url, included, updated_at) VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE included = VALUES(included), updated_at = VALUES(updated_at)`,
		url, included, at.UTC())
	if err != nil {
		return fmt.Errorf("set judgement for %s: %w", url, translate(err))
	}
	return nil
}

// Corpus is what a visitor's question is actually searched against.
//
// The two counts travel together and are meaningless apart, and a pair of bare
// integers in a return list is a pair a caller can swap without the compiler
// noticing. PublishResult is the same shape for the same reason.
type Corpus struct {
	// Pages is the number of published pages on included addresses.
	Pages int
	// Chunks is the number of chunks those pages hold.
	Chunks int
}

// LiveCounts reports the size of the corpus, so an administrator can tell
// whether a publish did what they expected.
//
// It counts the corpus rather than the table: a published page on an excluded
// address is not searched, so it is not counted here either.
func (d *DB) LiveCounts(ctx context.Context) (Corpus, error) {
	var c Corpus
	row := d.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT d.id), COUNT(c.id)
  FROM documents d
  LEFT JOIN address_settings s ON s.url = d.url
  LEFT JOIN chunks c ON c.document_id = d.id
 WHERE d.state = 'published' AND COALESCE(s.included, TRUE)`)
	if err := row.Scan(&c.Pages, &c.Chunks); err != nil {
		return Corpus{}, fmt.Errorf("count the corpus: %w", translate(err))
	}
	return c, nil
}

// nullString writes an empty string as NULL, because the columns it is used
// for are nullable and no reader distinguishes the two.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// timeToColumn writes an absent time as NULL and a present one in UTC, which
// is the only zone any time in this database is ever written in.
func timeToColumn(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}
