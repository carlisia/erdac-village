// The values that cross this package's boundary, and the checks that refuse
// one before the server ever sees it.
//
// Nothing here is database-shaped: no nullable-column wrappers, no driver
// values, no result sets. Ordinary Go types and UTC times, with a pointer only
// where the absence of a value means something a zero value does not.

package store

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/carlisia/erdac-village/internal/config"
)

// MaxAddressLength is the width of every address column, in characters.
//
// MySQL refuses to index a TEXT column without a key length, and 768 is the
// largest utf8mb4 value that fits an index key. An address longer than this is
// a failed page rather than a truncated one, because a truncated address is a
// different address, so it is refused here before the server sees it and also
// mapped from the server's own refusal.
const MaxAddressLength = 768

// Page is a fetched page on its way into the documents table: the cleaned text
// of one address at one point in time.
//
// It carries only what a caller supplies. The identifier, the state and the
// publish time are the database's to set, and a listing reads them back as a
// PageSummary, so putting them here would invite a caller to set them.
//
// Title is a nullable column and an empty string is indistinguishable from an
// absent one to every reader, so it is a plain string. LastMod is a pointer
// because its absence means something a zero time does not: the site published
// no modification date at all.
type Page struct {
	URL         string
	Title       string
	Markdown    string
	ContentHash string
	LastMod     *time.Time
	FetchedAt   time.Time
}

// PageSummary is what a listing needs about one page: enough to show it and to
// decide whether it changed, without carrying its whole body.
type PageSummary struct {
	ID          int64
	Title       string
	ContentHash string
	FetchedAt   time.Time
	PublishedAt *time.Time
}

// AddressState is everything the system knows about one address.
//
// Included carries the administrator's standing judgement, which is held
// against the address rather than against any page, so a newly fetched
// candidate inherits it. An address with no recorded judgement is included.
//
// Published and Candidate are nil when the address holds no page in that
// state. Both may be set at once, which is the whole point of the review
// workflow: the assistant keeps answering from the published page while an
// administrator reviews the candidate.
type AddressState struct {
	URL       string
	Included  bool
	Published *PageSummary
	Candidate *PageSummary
}

// Chunk is a slice of one page's text, sized for embedding.
type Chunk struct {
	Ordinal     int
	HeadingPath string
	Text        string
	TokenCount  int
}

// Embedding is one chunk's vector.
type Embedding struct {
	ChunkID int64
	Vector  []float32
}

// SystemState is the single row that holds what is true of the running system.
//
// FetchLock is empty when no fetch holds it. The three times are pointers
// because "never" is a state an administrator needs to see: it is how
// "nothing changed" is told apart from "nothing was ever reviewed".
type SystemState struct {
	Halted          bool
	SessionEpoch    int
	FetchLock       string
	FetchStartedAt  *time.Time
	LastFetchedAt   *time.Time
	LastPublishedAt *time.Time
}

// checkAddress refuses an address the column cannot hold, before the server
// sees it, so the caller gets the same error whether or not the server happens
// to be running in strict mode.
//
// The length is measured in characters rather than bytes because the column is
// declared in characters. A multi-byte address that fits in 768 characters is
// storable even though it is longer than 768 bytes.
func checkAddress(url string) error {
	if n := utf8.RuneCountInString(url); n > MaxAddressLength {
		return fmt.Errorf("%w: %d characters, limit %d", ErrAddressTooLong, n, MaxAddressLength)
	}
	return nil
}

// formatVector renders an embedding as the literal the vector column accepts.
//
// The embedding has to be written as a string literal because the two
// extensions do not compose: nothing casts the dimensionless vector that the
// string constructor returns into the fixed-width vector a column is declared
// as, so a vector produced inside the database cannot be stored from SQL at
// all. It round-trips through Go instead.
//
// The result is concatenated into the statement rather than bound, and that is
// safe by construction rather than by trust: every element is a float that
// this function formats, so no caller-supplied text reaches the statement.
//
// Formatted without an exponent. The shortest representation of a float uses
// exponent notation below roughly one ten-thousandth, and a normalised
// embedding is full of values that small. This was chosen before the server
// had been asked whether it parses an exponent; measured on 2026-09-08, it
// does, in both the column and the string constructor, and it emits one on
// the way out. The decimal form is kept because it is proven by the live tier
// and pinned by a fast test, and the cost is a few characters per element.
func formatVector(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 13)
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// checkVector refuses an embedding of the wrong width. The column is declared
// at exactly one width, so a wrong one is refused here with a message naming
// both numbers rather than by the server with a message naming neither.
func checkVector(v []float32) error {
	if len(v) != config.MaxVectorDimensions {
		return fmt.Errorf("%w: %d elements, column is %d",
			ErrWrongVectorWidth, len(v), config.MaxVectorDimensions)
	}
	return nil
}

// timeFromColumn converts a nullable column into the pointer a domain struct
// carries, in UTC, which is the only zone anything here is read or written in.
func timeFromColumn(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	utc := t.Time.UTC()
	return &utc
}
