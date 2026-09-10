// These care about what ends up in a piece, not how the splitting is arranged.
// Each is the original's, adjusted for tokens that are estimated from words
// rather than counted.
package chunk

import (
	"errors"
	"strings"
	"testing"
)

func opts() Options {
	return Options{
		SplitOn:          []string{"h2", "h3"},
		MinTokens:        100,
		MaxTokens:        800,
		OverlapRatio:     0.15,
		TokensPerWord:    1.4,
		ProviderTokenCap: 2048,
	}
}

func words(w string, n int) string { return strings.TrimSpace(strings.Repeat(w+" ", n)) }

func split(t *testing.T, md string, o Options) []Chunk {
	t.Helper()
	pieces, err := Split(md, o)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	return pieces
}

func TestSplitsAtTheConfiguredHeadingLevels(t *testing.T) {
	md := "## First\n\n" + words("alpha", 200) + "\n\n## Second\n\n" + words("beta", 200)
	pieces := split(t, md, opts())
	if len(pieces) < 2 {
		t.Fatalf("got %d pieces, want at least 2", len(pieces))
	}
	var alpha, beta bool
	for _, p := range pieces {
		alpha = alpha || strings.Contains(p.Text, "alpha")
		beta = beta || strings.Contains(p.Text, "beta")
	}
	if !alpha || !beta {
		t.Error("a section's text is missing from every piece")
	}
}

func TestDoesNotSplitAtDeeperHeadingsThanConfigured(t *testing.T) {
	md := "## Section\n\n" + words("alpha", 150) + "\n\n#### Aside\n\n" + words("beta", 150)
	for _, p := range split(t, md, opts()) {
		if strings.Contains(p.HeadingPath, "Aside") {
			t.Errorf("a level-four heading became part of a trail: %q", p.HeadingPath)
		}
	}
}

func TestEachPieceCarriesTheHeadingsItSatUnder(t *testing.T) {
	md := "## Services\n\n" + words("alpha", 150) + "\n\n### Strategy\n\n" + words("beta", 150)
	var services, strategy bool
	for _, p := range split(t, md, opts()) {
		services = services || strings.Contains(p.HeadingPath, "Services")
		strategy = strategy || p.HeadingPath == "Services > Strategy"
	}
	if !services || !strategy {
		t.Error("the heading trail is not carried, or a nested heading does not extend it")
	}
}

// A heading with one line under it is not worth retrieving by itself; it
// carries too little to answer anything.
func TestAShortSectionMergesRatherThanStandingAlone(t *testing.T) {
	md := "## Tiny\n\nJust a line.\n\n## Real\n\n" + words("alpha", 300)
	pieces := split(t, md, opts())
	var kept bool
	for i, p := range pieces {
		if p.TokenCount < 100 && i != len(pieces)-1 {
			t.Errorf("piece %d has %d tokens, under the floor, and is not the last", i, p.TokenCount)
		}
		kept = kept || strings.Contains(p.Text, "Just a line.")
	}
	if !kept {
		t.Error("the short section's text was lost in the merge")
	}
}

func TestALongSectionSplitsBelowTheCeiling(t *testing.T) {
	o := opts()
	o.MaxTokens = 400
	pieces := split(t, "## Long\n\n"+words("word", 4000), o)
	if len(pieces) < 2 {
		t.Fatalf("a section far over the ceiling produced %d piece", len(pieces))
	}
	for i, p := range pieces {
		if p.TokenCount > 400 {
			t.Errorf("piece %d estimates %d tokens, over the ceiling of 400", i, p.TokenCount)
		}
	}
}

func TestSplitPiecesOverlapSoASentenceIsNotCutInHalf(t *testing.T) {
	o := opts()
	o.MaxTokens = 400
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		b.WriteString("w")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteByte(' ')
	}
	// distinct words, so overlap can be seen
	ws := make([]string, 3000)
	for i := range ws {
		ws[i] = "w" + strings.Repeat("i", i%50) + string(rune('a'+i%26))
	}
	pieces := split(t, "## Long\n\n"+strings.Join(ws, " "), o)
	if len(pieces) < 2 {
		t.Fatal("expected a split")
	}
	tail := strings.Fields(pieces[0].Text)
	tail = tail[len(tail)-20:]
	var shared bool
	for _, w := range tail {
		if strings.Contains(pieces[1].Text, w) {
			shared = true
			break
		}
	}
	if !shared {
		t.Error("the second piece shares nothing with the first's tail, so there is no overlap")
	}
}

func TestNoContentIsLost(t *testing.T) {
	md := "## One\n\n" + words("alpha", 200) + "\n\n## Two\n\ndistinctive-marker-here"
	var found bool
	for _, p := range split(t, md, opts()) {
		found = found || strings.Contains(p.Text, "distinctive-marker-here")
	}
	if !found {
		t.Error("a trailing short section was dropped")
	}
}

func TestOrdinalsRunInOrderFromZero(t *testing.T) {
	md := "## A\n\n" + words("alpha", 200) + "\n\n## B\n\n" + words("beta", 200)
	for i, p := range split(t, md, opts()) {
		if p.Ordinal != i {
			t.Errorf("piece %d has ordinal %d", i, p.Ordinal)
		}
	}
}

func TestEmptyInputProducesNothing(t *testing.T) {
	for _, in := range []string{"", "   \n\n  "} {
		if pieces := split(t, in, opts()); len(pieces) != 0 {
			t.Errorf("%q produced %d pieces", in, len(pieces))
		}
	}
}

func TestTokenCountIsReportedAndPositive(t *testing.T) {
	for _, p := range split(t, "## A\n\n"+words("alpha", 200), opts()) {
		if p.TokenCount <= 0 {
			t.Errorf("piece %d reports %d tokens", p.Ordinal, p.TokenCount)
		}
	}
}

// The estimate rounds up. Under-counting produces a piece the provider
// refuses; over-counting produces a slightly smaller piece.
func TestTheEstimateRoundsUp(t *testing.T) {
	if got := Estimate("one two three", 1.4); got != 5 {
		t.Errorf("3 words at 1.4 estimated %d tokens, want 5 (4.2 rounded up)", got)
	}
	if got := Estimate("", 1.4); got != 0 {
		t.Errorf("no words estimated %d tokens", got)
	}
}

// The ceiling is validated to sit under the cap, so a piece over the cap means
// the bounds or the estimate are wrong. It must be an error, never a trim.
func TestAPieceOverTheProviderCapIsAnError(t *testing.T) {
	o := opts()
	o.MaxTokens = 3000
	o.ProviderTokenCap = 2048
	_, err := Split("## Big\n\n"+words("word", 2000), o)
	if !errors.Is(err, ErrOverCap) {
		t.Fatalf("got %v, want ErrOverCap", err)
	}
}

// A full piece's estimate must never exceed the ceiling, whatever the factor,
// because the cut is made in words and the estimate is made from words.
func TestAFullPieceNeverEstimatesOverTheCeiling(t *testing.T) {
	for _, factor := range []float64{1.0, 1.3, 1.4, 1.7, 2.5} {
		o := opts()
		o.TokensPerWord = factor
		o.MaxTokens = 300
		for _, p := range split(t, "## L\n\n"+words("w", 5000), o) {
			if p.TokenCount > o.MaxTokens {
				t.Errorf("factor %v: a piece estimates %d, ceiling %d", factor, p.TokenCount, o.MaxTokens)
			}
		}
	}
}

// An oversized section is cut at word boundaries, but the text between the
// words is the section's own. Paragraph breaks, a heading line and a list
// marker inside it must survive the cut, because the original kept them and
// a piece that has lost them reads as one run-on line.
func TestSplittingALongSectionKeepsItsStructure(t *testing.T) {
	o := opts()
	o.MaxTokens = 140
	md := "## Long\n\n" + words("alpha", 60) + "\n\n- an item\n\n#### Deep heading\n\n" + words("beta", 60)
	pieces := split(t, md, o)
	if len(pieces) < 2 {
		t.Fatal("expected the section to split")
	}
	joined := strings.Join(func() []string {
		var ts []string
		for _, p := range pieces {
			ts = append(ts, p.Text)
		}
		return ts
	}(), "\n")
	for _, want := range []string{"\n\n", "- an item", "#### Deep heading"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the cut lost %q", want)
		}
	}
}
