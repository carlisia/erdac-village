package crawl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Source is where pages come from. This is one of the two seams: it is
// declared here, beside the code that calls it, with the two methods that
// code needs and nothing more. The live implementation is an HTTP client; the
// test implementation reads sample pages from a folder.
type Source interface {
	Sitemap(ctx context.Context) ([]byte, error)
	Page(ctx context.Context, u string) ([]byte, error)
}

// maxPageBytes caps a download. A page larger than this is not a page, and
// reading it to the end would hold memory for nothing. The original had no
// cap; this one is recorded in the specification as an addition.
const maxPageBytes = 16 << 20

// HTTPSource is the live website.
type HTTPSource struct {
	client     *http.Client
	sitemapURL string
	userAgent  string
}

// NewHTTPSource builds the live source. Redirects are followed when the
// configuration says so, because the address a site gives out is often not
// the one that finally answers; a stub on this site does not redirect at all,
// which is why the frontier skips it by shape instead.
func NewHTTPSource(sitemapURL, userAgent string, timeout time.Duration, followRedirects bool) *HTTPSource {
	client := &http.Client{Timeout: timeout}
	if !followRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return &HTTPSource{client: client, sitemapURL: sitemapURL, userAgent: userAgent}
}

func (h *HTTPSource) Sitemap(ctx context.Context) ([]byte, error) {
	return h.get(ctx, h.sitemapURL)
}

func (h *HTTPSource) Page(ctx context.Context, u string) ([]byte, error) {
	return h.get(ctx, u)
}

func (h *HTTPSource) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", h.userAgent)
	res, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	// The body is read to the end below, and a close failure after a full read
	// carries nothing the caller could act on. Discarded rather than joined.
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("%s", res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxPageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPageBytes {
		return nil, fmt.Errorf("the response is larger than %d bytes", maxPageBytes)
	}
	return body, nil
}

// DirSource reads the sample pages off the disk. No network connection is
// opened, so the pipeline can be run and tested anywhere, and a failing test
// can never be mistaken for a real answer about a real site.
type DirSource struct {
	dir   string
	pages map[string]string
}

// NewDirSource maps address paths to files in a folder. The folder holds a
// sitemap.xml and the pages the map names.
func NewDirSource(dir string, pages map[string]string) *DirSource {
	return &DirSource{dir: dir, pages: pages}
}

func (d *DirSource) Sitemap(context.Context) ([]byte, error) {
	return os.ReadFile(filepath.Join(d.dir, "sitemap.xml"))
}

func (d *DirSource) Page(_ context.Context, u string) ([]byte, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}
	name, ok := d.pages[path]
	if !ok {
		return nil, fmt.Errorf("no sample page stands in for %s", path)
	}
	return os.ReadFile(filepath.Join(d.dir, name))
}

// FixturePages says which sample file stands in for which address in the
// sample sitemap. The sample pages are invented and each reproduces a hazard,
// so the map puts the right hazard behind the right kind of address.
//
// The page whose content never arrives is deliberately not here. It would
// make every sample run stop on the canary check, which is the behaviour it
// exists to prove and is proved in the tests instead. The addresses the
// frontier never downloads are mapped anyway, so a test that proves they are
// never asked for has something that would answer if they were.
var FixturePages = map[string]string{
	"/":                         "about.html",
	"/about/company":            "about.html",
	"/services/overview":        "hidden-section.html",
	"/guides/northwind":         "other-business.html",
	"/guides/acme":              "other-business.html",
	"/profiles/jane":            "other-business.html",
	"/policies/tracking-notice": "about.html",
	"/gatherings/spring-day":    "event-price.html",
	"/labels/go":                "about.html",
	"/notes":                    "stub.html",
	"/writing/index":            "folder.html",
	"/writing/explorer":         "attr-script.html",
}
