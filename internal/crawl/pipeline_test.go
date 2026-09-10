// The whole pipeline, run against the sample pages. No network and no
// database: the pages come off the disk and the results go into memory. Every
// test asserts something an administrator could see in the report, never how
// the pipeline is arranged inside.
package crawl

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/carlisia/erdac-village/internal/config"
	"github.com/carlisia/erdac-village/internal/store"
)

const htmlDir = "testdata/html"

var fixedNow = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join("testdata", "siteconfig.test.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// lengthEmbedder stands in for the provider: one vector per piece, shaped from
// the text, so a test can tell pieces apart.
type lengthEmbedder struct{ seen []string }

func (e *lengthEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.seen = append(e.seen, text)
	return []float32{float32(len(text))}, nil
}

// failingEmbedder refuses a piece containing a marker, the way the database
// function refuses when the provider does.
type failingEmbedder struct{ marker string }

func (e failingEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if strings.Contains(text, e.marker) {
		return nil, errors.New("Warning 3200: the provider refused")
	}
	return []float32{1}, nil
}

// recordingSource remembers which addresses were asked for, and can refuse
// some the way a broken page would.
type recordingSource struct {
	*DirSource
	asked  []string
	broken map[string]bool
}

func (r *recordingSource) Page(ctx context.Context, u string) ([]byte, error) {
	r.asked = append(r.asked, u)
	if r.broken[u] {
		return nil, errors.New("connection reset")
	}
	return r.DirSource.Page(ctx, u)
}

type runOpts struct {
	source   Source
	store    *MemoryStore
	embedder Embedder
	force    bool
	required []string
}

func run(t *testing.T, o runOpts) (Report, *MemoryStore) {
	t.Helper()
	if o.source == nil {
		o.source = NewDirSource(htmlDir, FixturePages)
	}
	if o.store == nil {
		o.store = NewMemoryStore(nil)
	}
	if o.embedder == nil {
		o.embedder = &lengthEmbedder{}
	}
	if o.required == nil {
		// The sample pages carry no canary text of their own, so a test that is
		// not about the canary check asks for nothing.
		o.required = []string{}
	}
	report, err := Run(context.Background(), o.source, o.store, o.embedder, Options{
		Config: testConfig(t), Owner: "test", Force: o.force, RequiredStrings: o.required,
		Now: func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return report, o.store
}

func urls(stored []Stored) []string {
	out := make([]string, len(stored))
	for i, s := range stored {
		out[i] = s.URL
	}
	sort.Strings(out)
	return out
}

func TestSavesThePagesTheSitemapLists(t *testing.T) {
	report, st := run(t, runOpts{})
	if len(report.Stored) == 0 {
		t.Fatal("nothing was stored")
	}
	var inStore []string
	for u := range st.Pages {
		inStore = append(inStore, u)
	}
	sort.Strings(inStore)
	if strings.Join(inStore, " ") != strings.Join(urls(report.Stored), " ") {
		t.Errorf("the store holds %v, the report says %v", inStore, urls(report.Stored))
	}
}

// Every entry the sitemap gave lands in exactly one bucket. A total that does
// not add up is a page somebody has to hunt for.
func TestTheReportAccountsForEveryEntry(t *testing.T) {
	report, _ := run(t, runOpts{})
	if !report.Accounted() {
		t.Errorf("the buckets do not add up to %d entries: %+v", report.Total, report)
	}
	if report.Total != 15 || len(report.Seen) != 12 || len(report.Duplicates) != 1 {
		t.Errorf("total %d, seen %d, duplicates %d; want 15, 12 and 1", report.Total, len(report.Seen), len(report.Duplicates))
	}
	if len(report.Malformed) != 2 || len(report.Robots) != 1 || len(report.ExcludedByPattern) != 5 ||
		len(report.StubsByShape) != 1 || len(report.Thin) != 1 || len(report.Stored) != 4 {
		t.Errorf("buckets: malformed %d robots %d pattern %d stubs %d thin %d stored %d",
			len(report.Malformed), len(report.Robots), len(report.ExcludedByPattern),
			len(report.StubsByShape), len(report.Thin), len(report.Stored))
	}
}

// Content about other businesses arrives as whole groups of addresses. Fed
// in, it makes the bot describe another business as this one.
func TestAnExcludedGroupNeverReachesTheStore(t *testing.T) {
	report, st := run(t, runOpts{})
	var seenGuides bool
	for _, u := range report.Seen {
		seenGuides = seenGuides || strings.Contains(u, "/guides/")
	}
	if !seenGuides {
		t.Fatal("the report does not list the excluded group at all")
	}
	for u := range st.Pages {
		if strings.Contains(u, "/guides/") || strings.Contains(u, "/profiles/") {
			t.Errorf("an excluded address was stored: %s", u)
		}
	}
	if strings.Contains(st.Markdown(), "Northwind Freight") {
		t.Error("text from a page about another business reached the store")
	}
}

// Surveyed: a one-segment address is a stub, a robots pattern is the site's
// own refusal, and neither is ever downloaded.
func TestStubsAndRobotsAddressesAreNeverAskedFor(t *testing.T) {
	src := &recordingSource{DirSource: NewDirSource(htmlDir, FixturePages)}
	report, _ := run(t, runOpts{source: src})
	for _, u := range src.asked {
		if strings.HasSuffix(u, "/notes") || strings.Contains(u, "/labels/") {
			t.Errorf("%s was downloaded", u)
		}
	}
	if len(report.StubsByShape) != 1 || !strings.HasSuffix(report.StubsByShape[0], "/notes") {
		t.Errorf("stubs = %v", report.StubsByShape)
	}
	if len(report.Robots) != 1 {
		t.Errorf("robots = %v", report.Robots)
	}
}

// A duplicate listing is one page. Counting it twice in the total and once
// anywhere else would make the report announce a fault the run did not commit.
func TestADuplicateSitemapEntryIsCountedOnce(t *testing.T) {
	report, st := run(t, runOpts{})
	if len(report.Duplicates) != 1 || report.Duplicates[0] != "https://example.test/about/company" {
		t.Errorf("duplicates = %v", report.Duplicates)
	}
	n := 0
	for _, s := range report.Stored {
		if s.URL == "https://example.test/about/company" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the duplicated page was stored %d times", n)
	}
	if _, ok := st.Pages["https://example.test/about/company"]; !ok {
		t.Error("the duplicated page was not stored at all")
	}
	if !report.Accounted() {
		t.Error("a duplicate broke the accounting")
	}
}

// A force run that lost a page has not replaced every page, so it must not
// tell the store it did. That claim is what clears a recorded takeover, and
// the address that failed may still hold a crashed run's candidate.
func TestOnlyAForceRunWithNoFailuresIsComplete(t *testing.T) {
	clean, _ := run(t, runOpts{force: true})
	if !clean.Complete(true) {
		t.Error("a force run with no failures is not complete")
	}
	if clean.Complete(false) {
		t.Error("a refresh reported itself complete")
	}
	src := &recordingSource{DirSource: NewDirSource(htmlDir, FixturePages),
		broken: map[string]bool{"https://example.test/about/company": true}}
	broken, _ := run(t, runOpts{source: src, force: true})
	if broken.Complete(true) {
		t.Error("a force run with a failed page reported itself complete")
	}
}

func TestAMalformedEntryIsNamedByItsExactText(t *testing.T) {
	report, _ := run(t, runOpts{})
	if len(report.Malformed) != 2 || report.Malformed[0] != "my-talk:-on-goroutines" {
		t.Errorf("malformed = %v", report.Malformed)
	}
}

// A folder listing arrives with an empty article and only links. Surveyed: it
// is what the word floor exists to drop, and it lands in its own bucket.
func TestAPageUnderTheWordFloorIsDroppedAsThin(t *testing.T) {
	report, st := run(t, runOpts{})
	if len(report.Thin) != 1 || !strings.HasSuffix(report.Thin[0], "/writing/index") {
		t.Errorf("thin = %v", report.Thin)
	}
	if _, stored := st.Pages["https://example.test/writing/index"]; stored {
		t.Error("a thin page was stored")
	}
}

func TestTextHiddenFromReadersNeverReachesTheStore(t *testing.T) {
	_, st := run(t, runOpts{})
	md := st.Markdown()
	if strings.Contains(md, "Unreleased offering") {
		t.Error("hidden text reached the store")
	}
	if !strings.Contains(md, "Enablement") {
		t.Error("visible text did not")
	}
}

func TestNavigationAndFootersNeverReachTheStore(t *testing.T) {
	_, st := run(t, runOpts{})
	if strings.Contains(st.Markdown(), "Readiness Score") {
		t.Error("navigation text reached the store")
	}
}

// A refresh that re-downloads everything gets avoided for being slow, and
// then the knowledge base goes stale instead.
func TestAnUnchangedPageIsNotProcessedAgain(t *testing.T) {
	_, first := run(t, runOpts{})
	report, _ := run(t, runOpts{store: NewMemoryStore(first.Held())})
	if len(report.Stored) != 0 {
		t.Errorf("a second run stored %v", urls(report.Stored))
	}
	var held []string
	for u := range first.Pages {
		held = append(held, u)
	}
	sort.Strings(held)
	skipped := append([]string(nil), report.Skipped...)
	sort.Strings(skipped)
	if strings.Join(held, " ") != strings.Join(skipped, " ") {
		t.Errorf("skipped %v, want every page the first run stored: %v", skipped, held)
	}
}

// The site's own dates are sometimes wrong. This is the way back from that.
func TestForceProcessesAPageThatDidNotChange(t *testing.T) {
	_, first := run(t, runOpts{})
	report, _ := run(t, runOpts{store: NewMemoryStore(first.Held()), force: true})
	if len(report.Skipped) != 0 {
		t.Errorf("force skipped %v", report.Skipped)
	}
	if len(report.Stored) != len(first.Pages) {
		t.Errorf("force stored %d pages, want %d", len(report.Stored), len(first.Pages))
	}
}

// A page whose date is missing is never treated as unchanged, whatever its
// fingerprint says, because an absent date proves nothing.
func TestAPageWithNoDateIsAlwaysProcessed(t *testing.T) {
	_, first := run(t, runOpts{})
	held := first.Held()
	for i := range held {
		held[i].Candidate.LastMod = nil
	}
	report, _ := run(t, runOpts{store: NewMemoryStore(held)})
	if len(report.Skipped) != 0 {
		t.Errorf("pages with no stored date were skipped: %v", report.Skipped)
	}
}

func TestAMissingCanaryStringStopsTheRun(t *testing.T) {
	st := NewMemoryStore(nil)
	_, err := Run(context.Background(), NewDirSource(htmlDir, FixturePages), st, &lengthEmbedder{}, Options{
		Config: testConfig(t), Owner: "test", RequiredStrings: []string{"no-page-says-this"},
	})
	if !errors.Is(err, ErrCanaryMissing) {
		t.Fatalf("got %v, want ErrCanaryMissing", err)
	}
	if !strings.Contains(err.Error(), `"no-page-says-this"`) {
		t.Errorf("the error does not name the missing text: %v", err)
	}
	// A knowledge base with holes in it looks exactly like a working one, so a
	// half-finished download must not be left behind for someone to publish.
	if len(st.Pages) != 0 {
		t.Errorf("%d pages were stored before the canary check", len(st.Pages))
	}
}

// The sample page whose content is built in the browser comes back empty
// while looking normal. Standing it in for a page that carries expected text
// is the only thing that reveals the loss.
func TestAPageAssembledInTheBrowserTripsTheCanaryCheck(t *testing.T) {
	present := NewDirSource(htmlDir, map[string]string{"/about/company": "about.html"})
	report, _ := run(t, runOpts{source: present, required: []string{"mid-market"}})
	if len(report.Stored) != 1 {
		t.Fatalf("the present case stored %d pages", len(report.Stored))
	}
	absent := NewDirSource(htmlDir, map[string]string{"/about/company": "js-rendered.html"})
	_, err := Run(context.Background(), absent, NewMemoryStore(nil), &lengthEmbedder{}, Options{
		Config: testConfig(t), Owner: "test", RequiredStrings: []string{"mid-market"},
	})
	if !errors.Is(err, ErrCanaryMissing) {
		t.Fatalf("a page built in the browser was not caught: %v", err)
	}
}

// Expected text often lives on a page that has not changed. Checking only the
// changed pages would stop a run that had nothing wrong with it.
func TestTheCanaryCheckReadsPagesThatWereSkipped(t *testing.T) {
	_, first := run(t, runOpts{})
	report, _ := run(t, runOpts{store: NewMemoryStore(first.Held()), required: []string{"mid-market"}})
	if len(report.Stored) != 0 || len(report.Skipped) == 0 {
		t.Errorf("stored %d, skipped %d; want 0 and some", len(report.Stored), len(report.Skipped))
	}
}

// An administrator's decision to leave a page out is stored, not worked out
// again, so it stands until they reverse it and costs nothing to keep.
func TestAnAdminExclusionIsNotDownloadedAgain(t *testing.T) {
	_, first := run(t, runOpts{})
	held := first.Held()
	sort.Slice(held, func(i, j int) bool { return held[i].URL < held[j].URL })
	dropped := held[0].URL
	held[0].Included = false

	src := &recordingSource{DirSource: NewDirSource(htmlDir, FixturePages)}
	report, st := run(t, runOpts{store: NewMemoryStore(held), source: src, force: true})
	if len(report.ExcludedByAdmin) != 1 || report.ExcludedByAdmin[0] != dropped {
		t.Errorf("excluded by admin = %v, want %s", report.ExcludedByAdmin, dropped)
	}
	if _, stored := st.Pages[dropped]; stored {
		t.Error("an excluded address was stored")
	}
	for _, u := range src.asked {
		if u == dropped {
			t.Error("an excluded address was downloaded")
		}
	}
}

// The rest of the site is still worth having, and the failure is reported
// rather than swallowed.
func TestOnePageThatWillNotDownloadDoesNotStopTheRest(t *testing.T) {
	src := &recordingSource{DirSource: NewDirSource(htmlDir, FixturePages),
		broken: map[string]bool{"https://example.test/about/company": true}}
	report, st := run(t, runOpts{source: src})
	if _, failed := report.Failed["https://example.test/about/company"]; !failed {
		t.Error("the broken page is not in the failures")
	}
	if _, stored := st.Pages["https://example.test/about/company"]; stored {
		t.Error("the broken page was stored")
	}
	if len(report.Stored) == 0 {
		t.Error("nothing else was stored")
	}
	if !report.Accounted() {
		t.Error("a failed page is not accounted for")
	}
}

// The embedding function reports failure by returning nothing. A page with
// any piece it refused is not stored at all, so a page is never searchable
// through half of itself, and the refusal is reported for that page.
func TestAPieceThatFailsToEmbedFailsItsPageAndStoresNothingOfIt(t *testing.T) {
	report, st := run(t, runOpts{embedder: failingEmbedder{marker: "operations-heavy"}})
	for u := range st.Pages {
		if strings.Contains(st.Pages[u].Page.Markdown, "operations-heavy") {
			t.Errorf("a page with a refused piece was stored: %s", u)
		}
	}
	var reported bool
	for u, why := range report.Failed {
		if strings.Contains(why, "provider refused") {
			reported = true
			if _, stored := st.Pages[u]; stored {
				t.Errorf("%s is both failed and stored", u)
			}
		}
	}
	if !reported {
		t.Error("the refusal was not reported")
	}
	if !report.Accounted() {
		t.Error("a page that failed to embed is not accounted for")
	}
}

// Vectors are paired to text by position. One missing anywhere would attach
// the meaning of one page to another and corrupt every search.
func TestEverySavedChunkGetsItsOwnVector(t *testing.T) {
	report, st := run(t, runOpts{})
	if st.VectorCount() != report.ChunkCount() || report.ChunkCount() == 0 {
		t.Errorf("%d vectors for %d chunks", st.VectorCount(), report.ChunkCount())
	}
}

func TestASavedPageIsACandidateWithTheRunsFetchTime(t *testing.T) {
	_, st := run(t, runOpts{})
	for u, p := range st.Pages {
		if !p.Page.FetchedAt.Equal(fixedNow) {
			t.Errorf("%s fetched at %v, want the run's time", u, p.Page.FetchedAt)
		}
	}
}

// Live pages the sitemap stopped listing are reported and never removed. A
// broken sitemap looks exactly like a page that was taken down.
func TestALivePageTheSitemapNoLongerListsIsReportedNotRemoved(t *testing.T) {
	gone := store.AddressState{URL: "https://example.test/old/page", Included: true,
		Published: &store.PageSummary{ID: 99, ContentHash: "x", FetchedAt: fixedNow}}
	report, _ := run(t, runOpts{store: NewMemoryStore([]store.AddressState{gone})})
	if len(report.Vanished) != 1 || report.Vanished[0] != gone.URL {
		t.Errorf("vanished = %v", report.Vanished)
	}
}

func TestASamplePageWithNoStandInIsReportedNotRaised(t *testing.T) {
	report, st := run(t, runOpts{source: NewDirSource(htmlDir, map[string]string{})})
	if len(st.Pages) != 0 {
		t.Error("something was stored with no pages to read")
	}
	if len(report.Failed) != 5 {
		t.Errorf("%d failures, want one per wanted address (5)", len(report.Failed))
	}
}
