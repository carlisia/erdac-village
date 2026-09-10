package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// Embedder turns text into vectors by asking the database to, one piece at a
// time, on one dedicated connection held for as long as the Embedder is open.
//
// Everything about this shape is forced and recorded in the porting notes. The
// embedding function is a database function that holds a server thread for
// the whole outbound call. The provider key must not appear in a statement,
// because a statement is visible in every query log, so it lives in a session
// variable. A session variable belongs to the one connection it was set on,
// and the pool hands out any free connection, so the connection is pinned. And
// the function reports failure by returning NULL with a warning rather than by
// raising, so every result is checked.
type Embedder struct {
	conn     *sqlx.Conn
	provider string
	model    string
}

// OpenEmbedder pins a connection, puts the key in a session variable on it,
// and sets the statement timeout there. The timeout matters because the
// function holds a server thread for the whole provider call and the server's
// default would give up on a slow provider.
//
// Close releases the connection. The key is gone with it: session variables
// do not survive the connection returning to the pool.
func (d *DB) OpenEmbedder(ctx context.Context, provider, model, key string, timeout time.Duration) (*Embedder, error) {
	conn, err := d.db.Connx(ctx)
	if err != nil {
		return nil, fmt.Errorf("pin a connection for embedding: %w", translate(err))
	}
	if _, err := conn.ExecContext(ctx, `SET @erdac_key = ?`, key); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("hold the provider key: %w", translate(err))
	}
	if _, err := conn.ExecContext(ctx, `SET SESSION max_execution_time = ?`, timeout.Milliseconds()); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("set the statement timeout: %w", translate(err))
	}
	return &Embedder{conn: conn, provider: provider, model: model}, nil
}

// Embed returns the vector for one piece of text.
//
// The result is wrapped in a character-set conversion because the function
// returns a binary string and nothing downstream accepts one. A NULL result is
// the function's way of failing; the warning it raised alongside is read back
// and carried in the error, because it is the only account of what went wrong.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	var raw sql.NullString
	err := e.conn.QueryRowContext(ctx,
		`SELECT CONVERT(ai_embedding(?, ?, @erdac_key, ?) USING utf8mb4)`,
		e.provider, e.model, text).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", translate(err))
	}
	if !raw.Valid {
		return nil, fmt.Errorf("embed: %w: %s", ErrEmbeddingFailed, e.lastWarning(ctx))
	}
	vector, err := parseVector(raw.String)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if err := checkVector(vector); err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	return vector, nil
}

// Close returns the pinned connection to the pool.
func (e *Embedder) Close() error {
	return e.conn.Close()
}

// lastWarning reads the warning the previous statement left. It is best
// effort: a failure to read it produces a message saying so rather than an
// error, because the caller already has the error that matters.
func (e *Embedder) lastWarning(ctx context.Context) string {
	var level string
	var code int
	var message string
	err := e.conn.QueryRowContext(ctx, `SHOW WARNINGS LIMIT 1`).Scan(&level, &code, &message)
	if err != nil {
		return "and the warning could not be read: " + err.Error()
	}
	return fmt.Sprintf("%s %d: %s", level, code, message)
}

// parseVector reads the text form the vector functions produce: a bracketed,
// comma-separated list of numbers. The server may spell a number with an
// exponent, measured, so each element goes through a real float parser.
func parseVector(text string) ([]float32, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "[") || !strings.HasSuffix(text, "]") {
		return nil, fmt.Errorf("the vector is not bracketed: %.40q", text)
	}
	inner := strings.TrimSpace(text[1 : len(text)-1])
	if inner == "" {
		return nil, fmt.Errorf("the vector is empty")
	}
	fields := strings.Split(inner, ",")
	out := make([]float32, len(fields))
	for i, f := range fields {
		v, err := strconv.ParseFloat(strings.TrimSpace(f), 32)
		if err != nil {
			return nil, fmt.Errorf("element %d of the vector is not a number: %q", i, f)
		}
		out[i] = float32(v)
	}
	return out, nil
}
