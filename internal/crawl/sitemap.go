// Package crawl reads a site and stores what changed as candidates for review.
//
// It is the order of operations and nothing else: read the sitemap, decide
// which addresses are worth downloading, download them a few at a time, turn
// each page into the text a reader sees, check that text the site is known to
// carry actually arrived, split each page into pieces, have the database embed
// each piece, and store every changed page as a candidate. Splitting and
// storing live in their own packages and are used here unchanged.
//
// Two things about the arrangement are deliberate. The network and the store
// arrive as arguments rather than being reached for directly, which is what
// lets the whole pipeline run against sample pages with no network and no
// database. And nothing is written until every downloaded page has been
// checked for the text it is known to carry, because a knowledge base with
// holes in it looks exactly like a working one until a visitor asks the wrong
// question.
package crawl

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Entry is one sitemap entry: an absolute address and, when the site
// supplied one, the time it says the page last changed.
type Entry struct {
	URL     string
	LastMod *time.Time
}

// Sitemap is what the sitemap file said, split into entries that are
// addresses and entries that are not.
//
// Malformed carries the exact text of each entry that is not an absolute
// address. Measured: a bare string containing a colon parses without error,
// the text before the colon becomes a scheme, and resolving it returns the
// string unchanged. Those entries are skipped and named, never repaired and
// never fatal. A guess fetches a page that may not be the one meant, and
// refusing the whole run for a typo in a generated file holds every other
// page hostage to it.
type Sitemap struct {
	Entries   []Entry
	Malformed []string
	// Duplicates is every address the sitemap listed more than once. The
	// first listing is kept; the rest are counted here so the report's
	// buckets still add up to the number of entries the file gave.
	Duplicates []string
}

type urlset struct {
	URLs []struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	} `xml:"url"`
}

// ParseSitemap reads the sitemap format. An entry with no address at all is
// ignored, as the original did, because there is nothing to name.
func ParseSitemap(raw []byte) (Sitemap, error) {
	var set urlset
	if err := xml.Unmarshal(raw, &set); err != nil {
		return Sitemap{}, fmt.Errorf("read the sitemap: %w", err)
	}
	var out Sitemap
	seen := map[string]bool{}
	for _, u := range set.URLs {
		loc := strings.TrimSpace(u.Loc)
		if loc == "" {
			continue
		}
		if !isAbsolute(loc) {
			out.Malformed = append(out.Malformed, loc)
			continue
		}
		if seen[loc] {
			out.Duplicates = append(out.Duplicates, loc)
			continue
		}
		seen[loc] = true
		out.Entries = append(out.Entries, Entry{URL: loc, LastMod: parseLastMod(u.LastMod)})
	}
	return out, nil
}

// isAbsolute is the check the parser does not make. A relative string with a
// colon in it is accepted by the parser as a scheme and an opaque part, so the
// test is for a web scheme and a host, not for a parse error.
func isAbsolute(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// parseLastMod reads a sitemap date. A date that cannot be read is no date,
// which means the page is downloaded again rather than skipped: the dates
// come from the site's own tool and are not always present or right.
func parseLastMod(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}
