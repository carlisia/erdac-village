// Command migrate brings a database from empty to the current schema and
// reports what it ran.
//
// The connection string is read from the environment rather than taken as an
// argument, because it carries a password and a command line is visible in the
// process list to every account on the machine.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/carlisia/erdac-village/internal/store"
)

// dsnVariable is the environment variable holding the connection string.
const dsnVariable = "VILLAGE_DSN"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv(dsnVariable)
	if dsn == "" {
		return fmt.Errorf("set %s to the connection string", dsnVariable)
	}
	ctx := context.Background()

	db, err := store.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() {
		// The migration has already succeeded or failed by the time this runs,
		// so a close failure cannot change the outcome. Reported rather than
		// returned, because returning it would replace the real error with a
		// less useful one.
		if err := db.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "migrate: close the database: %v\n", err)
		}
	}()

	ran, err := db.Migrate(ctx)
	if err != nil {
		return err
	}
	if len(ran) == 0 {
		fmt.Println("already current")
	}
	// The loop does nothing when nothing ran, so both outcomes reach the same
	// end of this function. An earlier version returned from inside the branch
	// above, which gave the function two success paths and meant any step added
	// below would silently be skipped for one of them.
	for _, m := range ran {
		fmt.Printf("applied %d %s\n", m.Version, m.Name)
	}
	return nil
}
