package crawl

import "testing"

// Invented, and the same set as the test configuration. Address groups belong
// in settings, never written into code or a test as a real site's.
var rules = Rules{
	Robots:  []string{"/labels/*"},
	Exclude: []string{"/guides/*", "/profiles/*", "/policies/tracking-*", "/gatherings/spring-day"},
	Excluded: func(u string) bool {
		return u == "https://example.test/writing/2024/old-post"
	},
}

func TestTheFrontier(t *testing.T) {
	for _, c := range []struct {
		url  string
		want Bucket
		why  string
	}{
		{"https://example.test/", Wanted, "the home page has no segment and is a real page"},
		{"https://example.test/about/company", Wanted, "nothing matches a nested real page"},
		{"https://example.test/labels/go", Robots, "the robots pattern"},
		{"https://example.test/labels/go/deeper", Robots, "a star matches across slashes, as in the shell"},
		{"https://example.test/guides/northwind", ExcludedByPattern, "a whole excluded group"},
		{"https://example.test/policies/tracking-notice", ExcludedByPattern, "a partial name inside a group"},
		{"https://example.test/gatherings/spring-day", ExcludedByPattern, "an exact address"},
		{"https://example.test/gatherings/other", Wanted, "an exact pattern matches nothing else"},
		{"https://example.test/notes", StubByShape, "one segment is a stub, surveyed"},
		{"https://example.test/Notes", StubByShape, "capitalisation does not matter, surveyed"},
		{"https://example.test/writing/2024/old-post", ExcludedByAdmin, "excluded during review"},
	} {
		if got := Classify(c.url, rules); got != c.want {
			t.Errorf("%s: bucket %d, want %d (%s)", c.url, got, c.want, c.why)
		}
	}
}

// An address matching several rules lands in the first, so the report never
// counts it twice and the order is the one an administrator would expect:
// the site's own refusal first, then the configured exclusions, then shape,
// then the review decision.
func TestARobotsMatchWinsOverEverythingElse(t *testing.T) {
	r := Rules{Robots: []string{"/x"}, Exclude: []string{"/x"}, Excluded: func(string) bool { return true }}
	if got := Classify("https://example.test/x", r); got != Robots {
		t.Errorf("got bucket %d, want Robots", got)
	}
}

func TestAnAdminExclusionIsNotConsultedForAnAddressAlreadyOut(t *testing.T) {
	asked := false
	r := Rules{Exclude: []string{"/gone/*"}, Excluded: func(string) bool { asked = true; return true }}
	Classify("https://example.test/gone/page", r)
	if asked {
		t.Error("the review decision was consulted for an address a pattern had already excluded")
	}
}
