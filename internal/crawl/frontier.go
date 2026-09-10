package crawl

import (
	"net/url"
	"regexp"
	"strings"
)

// Bucket is where an address landed when the frontier was decided. Every
// address lands in exactly one, and the report lists every bucket, so the
// counts add up to the sitemap's and a lost page is visible.
type Bucket int

const (
	// Wanted is downloaded.
	Wanted Bucket = iota
	// Robots matched a configured pattern the site's robots file disallows.
	Robots
	// ExcludedByPattern matched a configured excluded pattern or group.
	ExcludedByPattern
	// StubByShape has a single path segment. Surveyed: every alias stub on
	// this site has one segment and every real page has more, and every
	// stub's real target is listed separately, so skipping by shape loses
	// nothing. A stub does not redirect; it answers success with a refresh
	// instruction, so only a rule that runs before downloading can avoid it.
	StubByShape
	// ExcludedByAdmin was excluded during review. The decision is stored, so
	// it costs nothing to keep and stands until the administrator reverses it.
	ExcludedByAdmin
)

// Rules are the frontier's inputs: the configured patterns, and which
// addresses an administrator has excluded.
type Rules struct {
	Robots        []string
	Exclude       []string
	ExcludeGroups []string
	// Excluded reports whether an administrator excluded the address.
	Excluded func(url string) bool
}

// Classify decides the bucket for one address, testing the rules in a fixed
// order so that an address matching several lands in the first.
func Classify(u string, r Rules) Bucket {
	path := pathOf(u)
	switch {
	case matchesAny(path, r.Robots):
		return Robots
	case matchesAny(path, r.Exclude) || matchesAny(path, r.ExcludeGroups):
		return ExcludedByPattern
	case isSingleSegment(path):
		return StubByShape
	case r.Excluded != nil && r.Excluded(u):
		return ExcludedByAdmin
	}
	return Wanted
}

func pathOf(u string) string {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Path == "" {
		return "/"
	}
	return parsed.Path
}

// isSingleSegment reports a path with exactly one segment: "/notes" but not
// "/" and not "/writing/notes". The home page has no segment and is a real
// page; a nested address has more than one.
func isSingleSegment(path string) bool {
	trimmed := strings.Trim(path, "/")
	return trimmed != "" && !strings.Contains(trimmed, "/")
}

// matchesAny matches a path against patterns written the way an administrator
// thinks about a site: "/tags/*", "/policies/tracking-*", "/gatherings/day".
// A star matches anything including a slash, as it does in the shell-style
// matching the original used, so "/tags/*" covers everything under that
// group however deep. A pattern with a trailing slash matches with or without it.
func matchesAny(path string, patterns []string) bool {
	for _, p := range patterns {
		if globMatch(path, p) || globMatch(path, strings.TrimSuffix(p, "/")) {
			return true
		}
	}
	return false
}

func globMatch(path, pattern string) bool {
	if pattern == "" {
		return false
	}
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(path)
}
