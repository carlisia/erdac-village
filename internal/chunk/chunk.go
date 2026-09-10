// Package chunk splits a cleaned page into the pieces retrieval returns.
//
// A piece is what the model actually reads, so two bounds matter. Too small
// and it carries no answer; too large and it crowds out the other pieces
// retrieved alongside it. Sections below the floor merge into their
// neighbour, sections above the ceiling split with an overlap so a sentence
// is not cut in half.
//
// Splitting happens at the heading levels named in configuration rather than
// an assumed one. Some sites never use a top-level heading, and splitting on a
// heading that is never present produces one enormous unusable piece per page.
//
// Tokens are estimated, not counted. The embedding model's tokenizer has no Go
// implementation, so a piece's tokens are its word count multiplied by a
// configured factor biased to over-count. The floor and ceiling are therefore
// soft targets. The provider's input limit is not: a piece over it is refused
// outright, so a piece whose estimate exceeds the cap is a fault of this code
// and is reported as one rather than sent.
package chunk

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Chunk is one slice of a page, with the trail of headings it sat under so an
// answer can say where it came from. The glossary reserves "piece" for the
// administration pages; here and in the store it is a chunk.
type Chunk struct {
	Ordinal     int
	HeadingPath string
	Text        string
	TokenCount  int
}

// Options are the configured bounds. They mirror the chunking section of the
// configuration file and are validated there.
type Options struct {
	// SplitOn names the heading levels to split at, as "h2", "h3".
	SplitOn []string
	// MinTokens is the floor under which a section merges into the next.
	MinTokens int
	// MaxTokens is the ceiling above which a section splits.
	MaxTokens int
	// OverlapRatio is the fraction of MaxTokens repeated across a split.
	OverlapRatio float64
	// TokensPerWord converts a word count to an estimated token count.
	TokensPerWord float64
	// ProviderTokenCap is the provider's hard input limit.
	ProviderTokenCap int
}

// ErrOverCap reports a piece whose estimated tokens exceed the provider's hard
// limit. The bounds are validated so that this cannot happen, which is why it
// is an error rather than a silent trim: reaching it means the bounds or the
// estimate are wrong.
var ErrOverCap = errors.New("a piece exceeds the provider's token cap")

var heading = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)

// Split breaks a cleaned page into pieces.
func Split(markdown string, o Options) ([]Chunk, error) {
	sections := sections(markdown, levels(o.SplitOn))
	merged := mergeShort(sections, o)

	var out []Chunk
	for _, s := range merged {
		for _, text := range splitLong(s.text, o) {
			n := Estimate(text, o.TokensPerWord)
			if n > o.ProviderTokenCap {
				return nil, fmt.Errorf("%w: piece %d under %q estimates %d tokens, cap is %d",
					ErrOverCap, len(out), s.path, n, o.ProviderTokenCap)
			}
			out = append(out, Chunk{
				Ordinal:     len(out),
				HeadingPath: s.path,
				Text:        text,
				TokenCount:  n,
			})
		}
	}
	return out, nil
}

// Estimate is the token count a piece is assumed to have: its words times the
// configured factor, rounded up. Rounding up is the bias: the provider refuses
// input over its limit, so under-counting produces a refused piece and
// over-counting produces a slightly smaller one.
func Estimate(text string, tokensPerWord float64) int {
	return int(math.Ceil(float64(len(strings.Fields(text))) * tokensPerWord))
}

type section struct {
	path string
	text string
}

func levels(splitOn []string) map[int]bool {
	out := map[int]bool{}
	for _, h := range splitOn {
		if len(h) == 2 && h[0] == 'h' && h[1] >= '1' && h[1] <= '6' {
			out[int(h[1]-'0')] = true
		}
	}
	return out
}

// sections breaks the page at the configured headings, keeping the trail of
// headings each section sat under.
func sections(markdown string, levels map[int]bool) []section {
	var out []section
	var current []string
	trail := map[int]string{}
	path := ""

	flush := func() {
		body := strings.TrimSpace(strings.Join(current, "\n\n"))
		if body != "" {
			out = append(out, section{path: path, text: body})
		}
		current = current[:0]
	}

	for _, block := range strings.Split(markdown, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if m := heading.FindStringSubmatch(block); m != nil && levels[len(m[1])] {
			flush()
			level := len(m[1])
			for k := range trail {
				if k >= level {
					delete(trail, k)
				}
			}
			trail[level] = strings.TrimSpace(m[2])
			path = joinTrail(trail)
		}
		current = append(current, block)
	}
	flush()
	return out
}

func joinTrail(trail map[int]string) string {
	keys := make([]int, 0, len(trail))
	for k := range trail {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = trail[k]
	}
	return strings.Join(parts, " > ")
}

// mergeShort folds a section below the floor into the one after it. A heading
// with a single line under it is not worth retrieving alone. It keeps its
// text so nothing is lost, and inherits the following heading path so the
// merged piece is still locatable. A trailing carry joins the last section.
func mergeShort(in []section, o Options) []section {
	var out []section
	carry := ""
	for _, s := range in {
		body := s.text
		if carry != "" {
			body = carry + "\n\n" + s.text
		}
		carry = ""
		if Estimate(body, o.TokensPerWord) < o.MinTokens {
			carry = body
			continue
		}
		out = append(out, section{path: s.path, text: body})
	}
	if carry != "" {
		if len(out) > 0 {
			last := &out[len(out)-1]
			last.text = last.text + "\n\n" + carry
		} else if len(in) > 0 {
			out = append(out, section{path: in[0].path, text: carry})
		}
	}
	return out
}

// splitLong cuts an oversized section into pieces that overlap at the seam.
//
// The cut is by words, because tokens are estimated from words, but the text
// between the words is the section's own. Joining the words back with single
// spaces would lose every paragraph break, heading line and list marker inside
// an oversized section; the original decoded its tokens back to text and kept
// them, so this slices the original text at word boundaries instead.
//
// The ceiling in words is the ceiling in tokens divided by the factor, rounded
// down, so that a full piece's estimate never exceeds the ceiling.
func splitLong(text string, o Options) []string {
	if Estimate(text, o.TokensPerWord) <= o.MaxTokens {
		return []string{text}
	}
	spans := wordSpans(text)
	maxWords := int(math.Floor(float64(o.MaxTokens) / o.TokensPerWord))
	if maxWords < 1 {
		maxWords = 1
	}
	overlap := int(float64(maxWords) * o.OverlapRatio)
	if overlap > maxWords-1 {
		overlap = maxWords - 1
	}
	if overlap < 0 {
		overlap = 0
	}
	step := maxWords - overlap

	var out []string
	for start := 0; start < len(spans); start += step {
		end := start + maxWords
		if end > len(spans) {
			end = len(spans)
		}
		piece := strings.TrimSpace(text[spans[start][0]:spans[end-1][1]])
		if piece != "" {
			out = append(out, piece)
		}
		if end == len(spans) {
			break
		}
	}
	return out
}

// wordSpans returns the byte range of every word, in order, so a run of words
// can be cut out of the text with the whitespace between them intact.
func wordSpans(text string) [][2]int {
	var spans [][2]int
	start := -1
	for i, r := range text {
		if unicode.IsSpace(r) {
			if start >= 0 {
				spans = append(spans, [2]int{start, i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		spans = append(spans, [2]int{start, len(text)})
	}
	return spans
}
