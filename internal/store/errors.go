// The failures this package tells a caller apart, and the translation from a
// driver error into one of them.

package store

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
)

// The failures a caller has to tell apart. Each is returned wrapped, so a
// caller matches with errors.Is and still reads a message naming the address
// or the operation that produced it.
var (
	// ErrFetchInProgress reports that another fetch holds the lock and has not
	// held it long enough to be treated as abandoned.
	ErrFetchInProgress = errors.New("a fetch is already running")

	// ErrFetchLockLost reports that the lock a caller is releasing is no
	// longer theirs, because it expired and another fetch took it. The caller
	// must not stamp a fetch time it did not finish.
	ErrFetchLockLost = errors.New("the fetch lock is no longer held by this run")

	// ErrPublishDuringFetch reports that a publish was attempted while a fetch
	// was running, which would promote a half-written candidate set.
	ErrPublishDuringFetch = errors.New("publish is refused while a fetch is running")

	// ErrAbandonedFetch reports that the last fetch never released the lock,
	// so it did not finish and the candidate set it left is partial.
	//
	// Expiring the lock lets a new fetch start, which is what keeps a crashed
	// run from locking the system out. It does not make the pages that run
	// wrote trustworthy. Publishing them would promote half of one crawl, so
	// publish refuses until a fetch runs to completion and replaces them.
	ErrAbandonedFetch = errors.New("the last fetch did not finish, so its candidate set is partial")

	// ErrTakeoverPending reports that a fetch took over an abandoned lock, so
	// the candidate set may hold pages from two runs. Publish refuses until a
	// fetch run in force mode completes, which is the only condition under
	// which every waiting page is known to come from one run.
	ErrTakeoverPending = errors.New("a fetch took over an abandoned lock; publish waits for a completed force fetch")

	// ErrEmbeddingFailed reports that the database's embedding function
	// returned nothing. It does not raise; it returns NULL and a warning, and
	// this carries the warning's text so the reason reaches a reader.
	ErrEmbeddingFailed = errors.New("the embedding function returned nothing")

	// ErrHalted reports that an administrator has halted the assistant.
	//
	// Nothing in this package returns it. It is declared beside SetHalted,
	// which owns the flag it describes, so that the answering path returns
	// this error rather than inventing a second one meaning the same thing.
	ErrHalted = errors.New("the assistant is halted")

	// ErrDuplicateCandidate and ErrDuplicatePublished wrap the two uniqueness
	// rules the database enforces through its generated columns, so a caller
	// never has to recognise a driver error number.
	ErrDuplicateCandidate = errors.New("the address already holds a candidate")
	ErrDuplicatePublished = errors.New("the address already holds a published page")

	// ErrAddressTooLong reports an address wider than the column allows.
	ErrAddressTooLong = errors.New("the address is longer than the column allows")

	// ErrWrongVectorWidth reports an embedding that is not the width the
	// column is declared at. The column takes exactly one width and the
	// embedding function offers no way to ask for another, so a different
	// width is a bug rather than a configuration choice.
	ErrWrongVectorWidth = errors.New("the embedding is not the width the column is declared at")

	// ErrNoSystemState reports that the single system-state row is missing,
	// which means the migration's seed did not run.
	ErrNoSystemState = errors.New("the system state row is missing")

	// ErrConflict reports a failure the database resolved by refusing this
	// caller rather than by rejecting its data: a deadlock it broke by rolling
	// one side back, or a row lock this caller waited too long for.
	//
	// Retrying is the correct response and the only one that works, because
	// nothing about the statement was wrong. It is a distinct sentinel for
	// that reason: every other error here means do not try this again.
	//
	// Publish holds the system-state row for its whole duration, so a fetch
	// starting during a long publish is the way this is most likely to be met.
	ErrConflict = errors.New("the database refused this caller to resolve a conflict")

	// ErrVectorExtensionMissing reports that vsql_vector is not installed. The
	// schema declares a vector column, so applying it without the extension
	// fails part-way through on a type the server does not recognise, leaving
	// the earlier tables created and the error naming a type rather than a
	// missing extension. Migrate checks first and refuses instead.
	ErrVectorExtensionMissing = errors.New("vsql_vector is not installed on this server")

	// ErrVectorTooNarrow reports that the server's widest vector is narrower
	// than the width the embedding model returns, so no column could hold one.
	ErrVectorTooNarrow = errors.New("the server's maximum vector width is below the embedding width")
)

// MySQL error numbers this package translates. Named because a bare number in
// a condition says nothing about what it matched.
const (
	errDuplicateEntry   = 1062 // ER_DUP_ENTRY
	errDataTooLong      = 1406 // ER_DATA_TOO_LONG
	errLockWaitTimeout  = 1205 // ER_LOCK_WAIT_TIMEOUT
	errLockDeadlock     = 1213 // ER_LOCK_DEADLOCK
	errFunctionNotFound = 1305 // ER_SP_DOES_NOT_EXIST
)

// translate turns a driver error into a domain one where a caller has to tell
// the difference, and returns everything else unchanged.
//
// The driver error is joined with %v rather than %w, so it reaches a reader in
// the message and is deliberately not reachable with errors.As. That is the
// whole point of translating: a caller that could still pull the driver error
// out would start matching on error numbers, which is what these sentinels
// exist to stop. The one cost is that a caller cannot inspect the original,
// and nothing here has ever needed to.
//
// The two uniqueness rules are told apart by the index named in the server's
// message, which is why both indexes carry names that say what they enforce.
// Matching on the name rather than on the column list is what keeps this
// working when the generated columns are rewritten.
func translate(err error) error {
	var me *mysql.MySQLError
	if !errors.As(err, &me) {
		return err
	}
	switch me.Number {
	case errDuplicateEntry:
		switch {
		case strings.Contains(me.Message, "documents_one_candidate_per_url"):
			return fmt.Errorf("%w: %v", ErrDuplicateCandidate, err)
		case strings.Contains(me.Message, "documents_one_published_per_url"):
			return fmt.Errorf("%w: %v", ErrDuplicatePublished, err)
		}
	case errLockDeadlock, errLockWaitTimeout:
		// The database broke a deadlock or gave up waiting for a row lock.
		// Nothing about the statement was wrong, so a caller that treats this
		// like invalid data retries nothing and reports a fault that does not
		// exist.
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case errDataTooLong:
		// The server names the column it refused. Only the address columns
		// mean this error: an overlong title reported as an overlong address
		// sends a caller to look at the wrong field.
		if strings.Contains(me.Message, "column 'url'") {
			return fmt.Errorf("%w: %v", ErrAddressTooLong, err)
		}
	}
	return err
}
