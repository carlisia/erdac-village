package crawl

import (
	"strings"
	"testing"
	"time"
)

func parsed(t *testing.T) Sitemap {
	t.Helper()
	s, err := ParseSitemap(readHTML(t, "sitemap.xml"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReadsEveryAddressOnce(t *testing.T) {
	s := parsed(t)
	if len(s.Entries) != 12 {
		t.Fatalf("read %d addresses, want 12 (one is listed twice)", len(s.Entries))
	}
	if len(s.Duplicates) != 1 {
		t.Errorf("duplicates = %v, want the one repeated address", s.Duplicates)
	}
	if s.Entries[0].URL != "https://example.test/" {
		t.Errorf("first entry is %q", s.Entries[0].URL)
	}
}

func TestReadsTheLastModifiedDateWhenPresent(t *testing.T) {
	for _, e := range parsed(t).Entries {
		if strings.HasSuffix(e.URL, "/about/company") {
			if e.LastMod == nil {
				t.Fatal("the date was dropped")
			}
			want := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
			if !e.LastMod.Equal(want) {
				t.Errorf("date = %v, want %v", e.LastMod, want)
			}
			return
		}
	}
	t.Fatal("the entry was not found")
}

// Dates come from the site's own tool and are not always there.
func TestSurvivesAnEntryWithNoDate(t *testing.T) {
	for _, e := range parsed(t).Entries {
		if strings.Contains(e.URL, "tracking-notice") {
			if e.LastMod != nil {
				t.Errorf("an undated entry got a date: %v", e.LastMod)
			}
			return
		}
	}
	t.Fatal("the entry was not found")
}

// Surveyed: some entries are bare text with a colon, which a parser reads as
// a scheme without complaint. They are named by their exact text so the
// sitemap can be fixed, and they are never resolved into an address.
func TestAnEntryThatIsNotAnAddressIsNamedNotRepaired(t *testing.T) {
	s := parsed(t)
	if len(s.Malformed) != 2 {
		t.Fatalf("named %d malformed entries, want 2: %v", len(s.Malformed), s.Malformed)
	}
	if s.Malformed[0] != "my-talk:-on-goroutines" {
		t.Errorf("the first malformed entry is %q, want its exact text", s.Malformed[0])
	}
	for _, e := range s.Entries {
		if strings.Contains(e.URL, "my-talk") || strings.Contains(e.URL, "another-talk") {
			t.Errorf("a malformed entry was repaired into %q", e.URL)
		}
	}
}

// A date the parser cannot read is no date, so the page is downloaded again
// rather than skipped on a date nobody can compare.
func TestAnUnreadableDateIsNoDate(t *testing.T) {
	s, err := ParseSitemap([]byte(`<urlset><url><loc>https://example.test/a/b</loc><lastmod>last tuesday</lastmod></url></urlset>`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Entries[0].LastMod != nil {
		t.Errorf("an unreadable date became %v", s.Entries[0].LastMod)
	}
}
