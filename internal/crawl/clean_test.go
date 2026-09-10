// Cleaning turns a downloaded page into the text an answer can be built from.
// Each test reproduces one hazard. They assert what a reader would see, never
// how the cleaning is implemented. All the pages are invented.
package crawl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var strip = []string{"nav", "footer", ".w-nav", ".footer_logo", ".w-condition-invisible"}

func readHTML(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "html", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func cleaned(t *testing.T, name string) Cleaned {
	t.Helper()
	c, err := Clean(readHTML(t, name), strip)
	if err != nil {
		t.Fatalf("clean %s: %v", name, err)
	}
	return c
}

func TestKeepsThePageTitle(t *testing.T) {
	if got := cleaned(t, "about.html").Title; got != "About Example Consulting" {
		t.Errorf("title = %q", got)
	}
}

func TestKeepsTheBodyText(t *testing.T) {
	md := cleaned(t, "about.html").Markdown
	for _, want := range []string{"helps mid-market companies", "operations-heavy sectors"} {
		if !strings.Contains(md, want) {
			t.Errorf("%q is missing", want)
		}
	}
}

// A promotional link repeated on every page otherwise lands in every piece
// and makes unrelated pages look relevant to questions about it.
func TestDropsNavigationAndFooter(t *testing.T) {
	md := cleaned(t, "about.html").Markdown
	for _, gone := range []string{"Readiness Score", "Copyright"} {
		if strings.Contains(md, gone) {
			t.Errorf("%q survived", gone)
		}
	}
}

// Conditional blocks keep their text in the markup. Left in, the bot quotes
// content nobody can see on the page.
func TestDropsSectionsHiddenFromReaders(t *testing.T) {
	md := cleaned(t, "hidden-section.html").Markdown
	if strings.Contains(md, "Unreleased offering") {
		t.Error("hidden text survived")
	}
	for _, want := range []string{"Strategy", "Enablement"} {
		if !strings.Contains(md, want) {
			t.Errorf("%q is missing", want)
		}
	}
}

func TestKeepsHeadingsSoChunkingHasSomethingToSplitOn(t *testing.T) {
	md := cleaned(t, "about.html").Markdown
	for _, want := range []string{"## What we do", "### How an engagement runs"} {
		if !strings.Contains(md, want) {
			t.Errorf("%q is missing", want)
		}
	}
}

// A page whose content is assembled in the browser comes back empty while the
// page around it looks normal. Nothing else reveals this.
func TestReportsWhenExpectedTextNeverArrived(t *testing.T) {
	md := cleaned(t, "js-rendered.html").Markdown
	got := MissingStrings(md, []string{"eight-pillar"})
	if len(got) != 1 || got[0] != "eight-pillar" {
		t.Errorf("missing = %v", got)
	}
}

func TestReportsNothingMissingWhenTheTextIsThere(t *testing.T) {
	if got := MissingStrings(cleaned(t, "about.html").Markdown, []string{"mid-market"}); len(got) != 0 {
		t.Errorf("missing = %v", got)
	}
}

func TestFingerprintIsStableForTheSameContent(t *testing.T) {
	first := cleaned(t, "about.html")
	second := cleaned(t, "about.html")
	if first.ContentHash != second.ContentHash {
		t.Error("two cleanings of one page disagree")
	}
}

func TestFingerprintChangesWhenTheContentDoes(t *testing.T) {
	if cleaned(t, "about.html").ContentHash == cleaned(t, "hidden-section.html").ContentHash {
		t.Error("two different pages share a fingerprint")
	}
}

func TestNeverReturnsRawHTML(t *testing.T) {
	for _, name := range []string{"about.html", "hidden-section.html", "event-price.html", "attr-script.html"} {
		md := strings.ReplaceAll(cleaned(t, name).Markdown, "<-", "")
		if strings.Contains(md, "<") {
			t.Errorf("%s: markup survived: %.120s", name, md)
		}
	}
}

// Site builders put a great deal of real content in unmarked containers
// rather than paragraphs. Reading only paragraphs and headings drops it
// silently, and the gap looks identical to a page that never said it.
func TestKeepsTextThatSitsInPlainContainers(t *testing.T) {
	md := cleaned(t, "div-text.html").Markdown
	for _, want := range []string{"eight-pillar", "How will the readiness score help me?"} {
		if !strings.Contains(md, want) {
			t.Errorf("%q is missing", want)
		}
	}
}

// A container and its parent both hold the same words. Emitting both doubles
// the text, which inflates the page against every question.
func TestDoesNotRepeatTextHeldByNestedContainers(t *testing.T) {
	md := cleaned(t, "div-text.html").Markdown
	if n := strings.Count(md, "eight-pillar"); n != 1 {
		t.Errorf("eight-pillar appears %d times", n)
	}
	if n := strings.Count(md, "A normal paragraph, for contrast."); n != 1 {
		t.Errorf("the paragraph appears %d times", n)
	}
}

// Designers wrap a word in a span to colour it. Reading only an element's own
// words splits the sentence in half and leaves a dangling fragment.
func TestKeepsASentenceWholeWhenPartOfItIsStyled(t *testing.T) {
	md := cleaned(t, "inline-split.html").Markdown
	for _, want := range []string{"AI for financial services", "AI for manufacturing", "a different starting point"} {
		if !strings.Contains(md, want) {
			t.Errorf("%q is missing or split", want)
		}
	}
}

func TestAFragmentNeverStandsAlone(t *testing.T) {
	for _, line := range strings.Split(cleaned(t, "inline-split.html").Markdown, "\n") {
		if strings.TrimSpace(line) == "AI for" {
			t.Fatal("a fragment stands on its own line")
		}
	}
}

// A container holding only a stylesheet looks like plain text to anything
// that reads a container's words.
func TestNeverEmitsStylesheetOrScriptText(t *testing.T) {
	md := cleaned(t, "style-inside.html").Markdown
	for _, gone := range []string{"overflow", "ellipsis"} {
		if strings.Contains(md, gone) {
			t.Errorf("stylesheet text %q survived", gone)
		}
	}
	if !strings.Contains(md, "Real words a reader can see.") {
		t.Error("the readable text is missing")
	}
}

func TestAHeadingKeepsItsSpacingAroundStyledWords(t *testing.T) {
	if !strings.Contains(cleaned(t, "inline-split.html").Markdown, "## Where we work") {
		t.Error("the heading lost its spacing or its marker")
	}
}

// A postal address is written as lines. A break carries no text of its own,
// so without treating it as a space the lines run together.
func TestALineBreakSeparatesWords(t *testing.T) {
	md := cleaned(t, "line-breaks.html").Markdown
	for _, want := range []string{"#150 Springfield", "Springfield CA, 90210"} {
		if !strings.Contains(md, want) {
			t.Errorf("%q is missing: %q", want, md)
		}
	}
}

// Surveyed: a pattern-based stripper leaked a function out of an attribute
// into body text. The parser must never let attribute contents through.
func TestScriptSourceInsideAnAttributeNeverBecomesText(t *testing.T) {
	md := cleaned(t, "attr-script.html").Markdown
	for _, gone := range []string{"isFolder", "=>", "filterFn", "startsWith"} {
		if strings.Contains(md, gone) {
			t.Errorf("attribute contents leaked into the text: %q", gone)
		}
	}
	for _, want := range []string{"Explore My Notes", "Text a reader can actually see"} {
		if !strings.Contains(md, want) {
			t.Errorf("%q is missing", want)
		}
	}
}

// A stub answers success with nothing a reader can see. Cleaned, it must be
// empty, so the word floor drops it if the frontier ever lets one through.
func TestAStubCleansToNothing(t *testing.T) {
	if md := cleaned(t, "stub.html").Markdown; md != "" {
		t.Errorf("a stub produced text: %q", md)
	}
}
