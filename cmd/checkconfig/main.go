// Command checkconfig loads and validates a configuration file and reports
// what is wrong with it.
//
// This exists so that the guarantee "the real siteconfig.toml still parses"
// is enforced by the check script rather than by a unit test. Unit tests use
// invented fixtures; a test that reads the live configuration fails for
// reasons that have nothing to do with the code under test.
package main

import (
	"fmt"
	"os"

	"github.com/carlisia/erdac-village/internal/config"
)

func main() {
	if len(os.Args) > 2 {
		fmt.Fprintf(os.Stderr, "usage: %s [path/to/siteconfig.toml]\n", os.Args[0])
		os.Exit(2)
	}
	path := "siteconfig.toml"
	if len(os.Args) == 2 {
		path = os.Args[1]
	}
	if _, err := config.Load(path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s parses and validates\n", path)
}
