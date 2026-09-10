package crawl

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/carlisia/erdac-village/internal/chunk"
	"github.com/carlisia/erdac-village/internal/config"
	"github.com/carlisia/erdac-village/internal/store"
)

// Store is where the results go. The second seam, declared here with the two
// methods this package calls. The live implementation is the store package's
// handle; the test implementation is MemoryStore.
type Store interface {
	AddressStates(ctx context.Context) ([]store.AddressState, error)
	StoreCandidate(ctx context.Context, owner string, p store.Page, chunks []store.Chunk, vectors [][]float32) (int64, error)
}

// Embedder turns one chunk's text into its vector. The live implementation
// asks the database to, on a pinned connection; the test implementation
// returns a vector shaped from the text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// ErrCanaryMissing reports that text the site is known to carry never
// arrived. The run stops and nothing is written, because the pages are almost
// certainly assembling that content in the visitor's browser, which a
// downloader never sees, and a knowledge base with holes in it looks exactly
// like a working one.
var ErrCanaryMissing = errors.New("text that must appear in the download never arrived")

// Options are what one run is told.
type Options struct {
	Config config.Config
	// Owner is the run's identifier, which the store checks against the fetch
	// lock on every write. The run neither takes nor releases the lock; the
	// caller does, so that the command line and the administration pages can
	// share one implementation and answer at once when a run is in progress.
	Owner string
	// Force processes every page even when nothing appears to have changed.
	// The dates come from the site's own tool and are sometimes wrong, and
	// this is the way back from that.
	Force bool
	// RequiredStrings replaces the configuration's canary text for one run.
	// nil means use the configuration's; an empty slice means check nothing.
	RequiredStrings []string
	// Now supplies the fetch time. Tests fix it.
	Now func() time.Time
}

// Stored is one page the run saved.
type Stored struct {
	URL        string
	Title      string
	DocumentID int64
	ChunkCount int
}

// Report is what one run did, in the words an administrator would use.
//
// Every entry the sitemap gave lands in exactly one of the lists below, and
// Accounted checks it. A total that does not add up is a fault the report
// makes visible rather than a page somebody has to hunt for.
type Report struct {
	// Total is how many entries the sitemap gave, including malformed ones.
	Total int
	// Seen is every entry that was an address, whatever became of it.
	Seen []string
	// Malformed is the exact text of every entry that was not an address.
	Malformed []string
	// Duplicates is every address the sitemap listed more than once; the
	// first listing counts elsewhere and the rest count here.
	Duplicates []string
	// The frontier's buckets: never downloaded.
	Robots            []string
	ExcludedByPattern []string
	StubsByShape      []string
	ExcludedByAdmin   []string
	// Downloaded, then set aside.
	Thin    []string
	Skipped []string
	// Downloaded and saved.
	Stored []Stored
	// Address to what went wrong. One page that will not download or embed
	// does not stop the run, because the rest of the site is still worth
	// having, and the failure is reported rather than swallowed.
	Failed map[string]string
	// Vanished is every published address the sitemap no longer lists.
	// Reported and nothing else: a sitemap that came back broken would
	// otherwise silently empty the knowledge base.
	Vanished []string
}

// ChunkCount is how many chunks the run stored across every page.
func (r Report) ChunkCount() int {
	n := 0
	for _, s := range r.Stored {
		n += s.ChunkCount
	}
	return n
}

// Complete reports whether a run replaced every page it could have: it ran in
// force mode, so nothing was skipped as unchanged, and no page failed. This
// is the one condition under which every waiting candidate is known to come
// from this run, and it is what a completed run tells the store when it
// releases the lock, because only such a run may clear a recorded takeover. A
// force run with one failed page leaves whatever candidate that address held
// before, which may be a crashed run's.
func (r Report) Complete(force bool) bool {
	return force && len(r.Failed) == 0
}

// Accounted reports whether every sitemap entry landed in exactly one list.
func (r Report) Accounted() bool {
	return r.Total == len(r.Malformed)+len(r.Duplicates)+len(r.Robots)+len(r.ExcludedByPattern)+
		len(r.StubsByShape)+len(r.ExcludedByAdmin)+len(r.Thin)+len(r.Skipped)+
		len(r.Stored)+len(r.Failed)
}

// Run downloads the site and saves what changed as candidates for review.
// Nothing here makes anything live.
func Run(ctx context.Context, src Source, st Store, emb Embedder, o Options) (Report, error) {
	cfg := o.Config
	now := o.Now
	if now == nil {
		now = time.Now
	}
	required := cfg.Crawl.RequiredStrings
	if o.RequiredStrings != nil {
		required = o.RequiredStrings
	}
	report := Report{Failed: map[string]string{}}

	raw, err := src.Sitemap(ctx)
	if err != nil {
		return report, fmt.Errorf("read the sitemap: %w", err)
	}
	sitemap, err := ParseSitemap(raw)
	if err != nil {
		return report, err
	}
	report.Total = len(sitemap.Entries) + len(sitemap.Malformed) + len(sitemap.Duplicates)
	report.Malformed = sitemap.Malformed
	report.Duplicates = sitemap.Duplicates

	states, err := st.AddressStates(ctx)
	if err != nil {
		return report, fmt.Errorf("read what is already held: %w", err)
	}
	held := map[string]store.AddressState{}
	for _, a := range states {
		held[a.URL] = a
	}

	// The frontier, decided before anything is downloaded.
	rules := Rules{
		Robots:        cfg.Crawl.RobotsDisallowed,
		Exclude:       cfg.Crawl.DefaultExclude,
		ExcludeGroups: cfg.Crawl.DefaultExcludeGroups,
		Excluded: func(u string) bool {
			a, ok := held[u]
			return ok && !a.Included
		},
	}
	var wanted []Entry
	seen := map[string]bool{}
	for _, e := range sitemap.Entries {
		report.Seen = append(report.Seen, e.URL)
		seen[e.URL] = true
		switch Classify(e.URL, rules) {
		case Robots:
			report.Robots = append(report.Robots, e.URL)
		case ExcludedByPattern:
			report.ExcludedByPattern = append(report.ExcludedByPattern, e.URL)
		case StubByShape:
			report.StubsByShape = append(report.StubsByShape, e.URL)
		case ExcludedByAdmin:
			report.ExcludedByAdmin = append(report.ExcludedByAdmin, e.URL)
		default:
			wanted = append(wanted, e)
		}
	}

	downloads := downloadAll(ctx, src, wanted, cfg.Crawl.Concurrency)

	// Everything that came down, whether or not it is going to be saved. The
	// canary check reads this: the expected text may well live on a page that
	// has not changed, or on one too thin to keep.
	var corpus []string
	type pending struct {
		entry Entry
		page  Cleaned
	}
	var todo []pending
	for _, d := range downloads {
		if d.err != nil {
			report.Failed[d.entry.URL] = d.err.Error()
			continue
		}
		page, err := Clean(d.body, cfg.Crawl.StripSelectors)
		if err != nil {
			report.Failed[d.entry.URL] = err.Error()
			continue
		}
		corpus = append(corpus, page.Markdown)
		if len(strings.Fields(page.Markdown)) < cfg.Crawl.MinBodyWords {
			report.Thin = append(report.Thin, d.entry.URL)
			continue
		}
		if a, ok := held[d.entry.URL]; ok && !o.Force && unchanged(d.entry, page, a) {
			report.Skipped = append(report.Skipped, d.entry.URL)
			continue
		}
		todo = append(todo, pending{entry: d.entry, page: page})
	}

	if missing := MissingStrings(strings.Join(corpus, "\n\n"), required); len(missing) > 0 {
		quoted := make([]string, len(missing))
		for i, m := range missing {
			quoted[i] = fmt.Sprintf("%q", m)
		}
		return report, fmt.Errorf("%w: %s. Nothing was saved", ErrCanaryMissing, strings.Join(quoted, ", "))
	}

	opts := chunk.Options{
		SplitOn:          cfg.Chunking.SplitOn,
		MinTokens:        cfg.Chunking.MinTokens,
		MaxTokens:        cfg.Chunking.MaxTokens,
		OverlapRatio:     cfg.Chunking.OverlapRatio,
		TokensPerWord:    cfg.Chunking.TokensPerWord,
		ProviderTokenCap: cfg.Chunking.ProviderTokenCap,
	}
	for _, p := range todo {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		chunked, err := chunk.Split(p.page.Markdown, opts)
		if err != nil {
			report.Failed[p.entry.URL] = err.Error()
			continue
		}
		// Every chunk is embedded before anything about the page is written,
		// so a page with a chunk the provider refused is not stored at all
		// rather than stored with a chunk search cannot find.
		chunks := make([]store.Chunk, len(chunked))
		vectors := make([][]float32, len(chunked))
		var failed error
		for i, c := range chunked {
			v, err := emb.Embed(ctx, c.Text)
			if err != nil {
				failed = fmt.Errorf("chunk %d: %w", c.Ordinal, err)
				break
			}
			chunks[i] = store.Chunk{Ordinal: c.Ordinal, HeadingPath: c.HeadingPath, Text: c.Text, TokenCount: c.TokenCount}
			vectors[i] = v
		}
		if failed != nil {
			report.Failed[p.entry.URL] = failed.Error()
			continue
		}
		id, err := st.StoreCandidate(ctx, o.Owner, store.Page{
			URL:         p.entry.URL,
			Title:       p.page.Title,
			Markdown:    p.page.Markdown,
			ContentHash: p.page.ContentHash,
			LastMod:     p.entry.LastMod,
			FetchedAt:   now().UTC(),
		}, chunks, vectors)
		if err != nil {
			report.Failed[p.entry.URL] = err.Error()
			continue
		}
		report.Stored = append(report.Stored, Stored{URL: p.entry.URL, Title: p.page.Title, DocumentID: id, ChunkCount: len(chunked)})
	}

	for u, a := range held {
		if a.Published != nil && !seen[u] {
			report.Vanished = append(report.Vanished, u)
		}
	}
	sort.Strings(report.Vanished)
	return report, nil
}

// unchanged reports whether a page can be skipped. Both the date and the
// fingerprint have to agree, and a missing date never counts as a match: the
// dates are generated by the site's own tool, so treating an absent one as
// "nothing changed" would skip pages that did. The comparison is against the
// candidate if one is waiting, otherwise the published page, so a second
// download during a review does not re-process text it already holds.
func unchanged(e Entry, page Cleaned, held store.AddressState) bool {
	ref := held.Candidate
	if ref == nil {
		ref = held.Published
	}
	if ref == nil || e.LastMod == nil || ref.LastMod == nil {
		return false
	}
	return e.LastMod.Equal(*ref.LastMod) && page.ContentHash == ref.ContentHash
}

type download struct {
	entry Entry
	body  []byte
	err   error
}

// downloadAll fetches the pages a few at a time and keeps the sitemap's order.
// The limit caps how many requests are in flight at once. It is not a gap
// between requests, and nothing here reads the site's robots file or waits
// for a stated crawl delay; that is recorded in the deferred work.
func downloadAll(ctx context.Context, src Source, entries []Entry, limit int) []download {
	if limit < 1 {
		limit = 1
	}
	out := make([]download, len(entries))
	gate := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, e := range entries {
		wg.Add(1)
		go func(i int, e Entry) {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			body, err := src.Page(ctx, e.URL)
			out[i] = download{entry: e, body: body, err: err}
		}(i, e)
	}
	wg.Wait()
	return out
}
