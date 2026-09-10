// Command ingest downloads the site, prepares it for review, and can publish.
//
//	ingest fetch [--config F] [--force] [--fixtures DIR] [--require TEXT]...
//	ingest review [--config F]
//	ingest publish [--config F]
//
// fetch reads the sitemap, downloads what is worth having, and stores what
// changed as candidates. Nothing it does reaches a visitor. With --fixtures it
// runs against a folder of sample pages with no network and no database and
// saves nothing. review lists the candidates waiting for a decision. publish
// makes every included candidate live in one step.
//
// The connection string and the provider key come from the environment, never
// from arguments, because a command line is visible in the process list.
//
// Exit codes are distinct so a script can tell them apart: 2 for no provider
// key, 3 for a fetch already running, 4 for canary text that never arrived,
// 1 for anything else.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/carlisia/erdac-village/internal/config"
	"github.com/carlisia/erdac-village/internal/crawl"
	"github.com/carlisia/erdac-village/internal/store"
)

const (
	dsnVariable = "VILLAGE_DSN"
	keyVariable = "VILLAGE_GOOGLE_KEY"

	exitNoKey   = 2
	exitLocked  = 3
	exitCanary  = 4
	exitFailure = 1
)

// requireFlags collects repeated --require values.
type requireFlags []string

func (r *requireFlags) String() string     { return fmt.Sprint([]string(*r)) }
func (r *requireFlags) Set(v string) error { *r = append(*r, v); return nil }

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ingest fetch|review|publish [flags]")
		return exitFailure
	}
	command, rest := args[0], args[1:]
	ctx := context.Background()

	switch command {
	case "fetch":
		fs := flag.NewFlagSet("ingest fetch", flag.ContinueOnError)
		configPath := fs.String("config", "siteconfig.toml", "the configuration file naming the site")
		force := fs.Bool("force", false, "process every page, even ones that look unchanged")
		fixtures := fs.String("fixtures", "", "run against sample pages in this folder; nothing is saved")
		var require requireFlags
		fs.Var(&require, "require", "text that must appear in the download; repeat for more")
		if err := fs.Parse(rest); err != nil {
			return exitFailure
		}
		cfg, err := config.Load(*configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitFailure
		}
		if *fixtures != "" {
			return fetchFixtures(ctx, cfg, *fixtures, *force, require)
		}
		return fetch(ctx, cfg, *force, require)
	case "review", "publish":
		// Neither reads the configuration or takes a flag; the store is all
		// they need, and a flag accepted and ignored is a lie about what runs.
		fs := flag.NewFlagSet("ingest "+command, flag.ContinueOnError)
		if err := fs.Parse(rest); err != nil {
			return exitFailure
		}
		if fs.NArg() > 0 {
			fmt.Fprintf(os.Stderr, "ingest %s takes no arguments\n", command)
			return exitFailure
		}
		if command == "review" {
			return review(ctx)
		}
		return publish(ctx)
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n", command)
	return exitFailure
}

// fetchFixtures is a dry run against sample pages. The sample pages are
// invented, so they do not carry the canary text a real site is checked for.
// Checking for it here would stop every run, so the check is off unless
// --require names text to look for.
func fetchFixtures(ctx context.Context, cfg config.Config, dir string, force bool, require []string) int {
	// The sample pages carry no canary text of their own, so the check is off
	// unless --require names text: an empty list, never nil, which would mean
	// the configuration's text and stop every sample run whose configuration
	// names one.
	if require == nil {
		require = []string{}
	}
	st := crawl.NewMemoryStore(nil)
	report, err := crawl.Run(ctx, crawl.NewDirSource(dir, crawl.FixturePages), st, zeroEmbedder{}, crawl.Options{
		Config: cfg, Owner: "fixtures", Force: force, RequiredStrings: require,
	})
	fmt.Println("Sample pages. Nothing was saved and no vectors were built.")
	if len(require) == 0 {
		fmt.Println("The check for missing text is off. Name text with --require.")
	}
	return finish(report, err)
}

// finish is the tail every fetch shares: a canary miss is loud and separate
// from any other failure, the report is printed, and a report whose buckets do
// not add up is a fault of this program rather than of the site.
func finish(report crawl.Report, err error) int {
	if errors.Is(err, crawl.ErrCanaryMissing) {
		fmt.Fprintf(os.Stderr, "\nSTOPPED. %v\n", err)
		return exitCanary
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitFailure
	}
	printReport(report)
	if !report.Accounted() {
		fmt.Fprintln(os.Stderr, "\nFAULT: the report's buckets do not add up to the sitemap's entries")
		return exitFailure
	}
	return 0
}

// zeroEmbedder stands in for the database on a dry run, where nothing is kept
// and nothing is searched. Never use it for a real fetch: every piece would
// look identical to every other.
type zeroEmbedder struct{}

func (zeroEmbedder) Embed(context.Context, string) ([]float32, error) { return []float32{0}, nil }

func fetch(ctx context.Context, cfg config.Config, force bool, require []string) int {
	key := os.Getenv(keyVariable)
	if key == "" {
		fmt.Fprintf(os.Stderr, "no provider key: set %s\n", keyVariable)
		return exitNoKey
	}
	db, err := open(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitFailure
	}
	defer closing("the database", db.Close)

	owner := newOwner()
	if err := db.AcquireFetchLock(ctx, owner); err != nil {
		if errors.Is(err, store.ErrFetchInProgress) {
			fmt.Fprintln(os.Stderr, "another fetch is already running")
			return exitLocked
		}
		fmt.Fprintln(os.Stderr, err)
		return exitFailure
	}
	// Released whatever happens. A finished run records its time; a failed
	// one releases without claiming a check it never completed.
	finished := false
	defer func() {
		if !finished {
			_ = db.ReleaseFetchLock(context.Background(), owner, nil, false)
		}
	}()

	// A running crawl renews its lock well inside the expiry, so a slow crawl
	// is never mistaken for a dead one. Losing the lock cancels the run, which
	// stops it writing another page.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go renew(runCtx, db, owner, cancel)

	embedder, err := db.OpenEmbedder(runCtx, cfg.Models.EmbeddingProvider, cfg.Models.EmbeddingModel, key,
		time.Duration(cfg.Crawl.TimeoutSeconds)*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitFailure
	}
	defer closing("the embedder", embedder.Close)

	source := crawl.NewHTTPSource(cfg.Site.Sitemap, cfg.Crawl.UserAgent,
		time.Duration(cfg.Crawl.TimeoutSeconds)*time.Second, cfg.Crawl.FollowRedirects)
	report, err := crawl.Run(runCtx, source, db, embedder, crawl.Options{
		Config: cfg, Owner: owner, Force: force, RequiredStrings: requireOrNil(require),
	})
	// The renewal stops before the lock is released, so a tick landing after
	// the release cannot report a lock lost on a run that finished.
	cancel()
	if err != nil {
		return finish(report, err)
	}

	// Last, so that only a run that got this far counts as a reading of the
	// site. Only a force run in which no page failed may say it replaced
	// every page, because that is what clears a recorded takeover.
	now := time.Now().UTC()
	if err := db.ReleaseFetchLock(context.Background(), owner, &now, report.Complete(force)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitFailure
	}
	finished = true

	code := finish(report, nil)
	if len(report.Vanished) > 0 {
		fmt.Println("\nLive pages the sitemap no longer lists. Nothing was removed;")
		fmt.Println("look at each one, because a broken sitemap looks like this too:")
		for _, u := range report.Vanished {
			fmt.Println("  " + u)
		}
	}
	if force && !report.Complete(force) {
		fmt.Println("\nThis force run did not replace every page, because some failed.")
		fmt.Println("If a takeover was recorded, publish stays refused until one does.")
	}
	return code
}

// renew keeps the lock alive while the run works, and cancels the run the
// moment the lock is no longer ours.
func renew(ctx context.Context, db *store.DB, owner string, cancel context.CancelFunc) {
	tick := time.NewTicker(store.FetchLockTTL / 3)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := db.RenewFetchLock(ctx, owner); errors.Is(err, store.ErrFetchLockLost) {
				fmt.Fprintln(os.Stderr, "the fetch lock was taken by another run; stopping")
				cancel()
				return
			}
		}
	}
}

func review(ctx context.Context) int {
	db, err := open(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitFailure
	}
	defer closing("the database", db.Close)
	states, err := db.AddressStates(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitFailure
	}
	var waiting []store.AddressState
	for _, a := range states {
		if a.Candidate != nil {
			waiting = append(waiting, a)
		}
	}
	if len(waiting) == 0 {
		fmt.Println("nothing is waiting for a decision")
		return 0
	}
	sort.Slice(waiting, func(i, j int) bool { return waiting[i].URL < waiting[j].URL })
	fmt.Printf("%d page(s) waiting for a decision:\n\n", len(waiting))
	for _, a := range waiting {
		mark := "include"
		if !a.Included {
			mark = "exclude"
		}
		changed := "new"
		if a.Published != nil {
			changed = "same"
			if a.Published.ContentHash != a.Candidate.ContentHash {
				changed = "changed"
			}
		}
		fmt.Printf("  [%7s] %7s  %s\n", mark, changed, a.URL)
		if a.Candidate.Title != "" {
			fmt.Printf("            %s\n", a.Candidate.Title)
		}
	}
	return 0
}

func publish(ctx context.Context) int {
	db, err := open(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitFailure
	}
	defer closing("the database", db.Close)
	result, err := db.Publish(ctx, time.Now().UTC())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, store.ErrPublishDuringFetch) || errors.Is(err, store.ErrAbandonedFetch) || errors.Is(err, store.ErrTakeoverPending) {
			return exitLocked
		}
		return exitFailure
	}
	fmt.Printf("published %d page(s), retired %d\n", result.Promoted, result.Retired)
	return 0
}

func open(ctx context.Context) (*store.DB, error) {
	dsn := os.Getenv(dsnVariable)
	if dsn == "" {
		return nil, fmt.Errorf("set %s to the connection string", dsnVariable)
	}
	return store.Open(ctx, dsn)
}

// closing reports a close failure to stderr rather than returning it. By the
// time a deferred close runs the command has already succeeded or failed, so
// a close failure cannot change the outcome, and returning it would replace
// the real result with a less useful one.
func closing(what string, close func() error) {
	if err := close(); err != nil {
		fmt.Fprintf(os.Stderr, "ingest: close %s: %v\n", what, err)
	}
}

// newOwner names one run. It only has to be unique among runs that could
// overlap, and it appears in the lock row, so it is short and unguessable
// rather than meaningful.
func newOwner() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return "run-" + hex.EncodeToString(b[:])
}

// requireOrNil keeps the distinction the pipeline makes: nil means use the
// configuration's canary text, an empty list means check nothing.
func requireOrNil(require []string) []string {
	if len(require) == 0 {
		return nil
	}
	return require
}

func printReport(r crawl.Report) {
	fmt.Printf("sitemap entries       %d\n", r.Total)
	fmt.Printf("not an address        %d\n", len(r.Malformed))
	fmt.Printf("listed more than once %d\n", len(r.Duplicates))
	fmt.Printf("left out by robots    %d\n", len(r.Robots))
	fmt.Printf("left out by address   %d\n", len(r.ExcludedByPattern))
	fmt.Printf("skipped as stubs      %d\n", len(r.StubsByShape))
	fmt.Printf("left out by an admin  %d\n", len(r.ExcludedByAdmin))
	fmt.Printf("too thin to keep      %d\n", len(r.Thin))
	fmt.Printf("unchanged, skipped    %d\n", len(r.Skipped))
	fmt.Printf("saved as candidates   %d (%d chunks)\n", len(r.Stored), r.ChunkCount())
	for _, p := range r.Stored {
		fmt.Printf("  %s  (%d chunks)\n", p.URL, p.ChunkCount)
	}
	if len(r.Malformed) > 0 {
		fmt.Println("\nsitemap entries that are not addresses, exactly as written:")
		for _, m := range r.Malformed {
			fmt.Printf("  %q\n", m)
		}
	}
	if len(r.Failed) > 0 {
		fmt.Println("\nwould not download or embed:")
		keys := make([]string, 0, len(r.Failed))
		for k := range r.Failed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %s: %s\n", k, r.Failed[k])
		}
	}
}
