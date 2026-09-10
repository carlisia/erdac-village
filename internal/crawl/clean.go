package crawl

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Cleaned is a downloaded page reduced to the text a reader sees.
type Cleaned struct {
	Title       string
	Markdown    string
	ContentHash string
}

// Headings become markdown so the chunker has something to split on. The
// level is taken from the tag rather than assumed; some sites never use h1.
var headings = map[string]string{"h1": "#", "h2": "##", "h3": "###", "h4": "####"}

var lineBreak = regexp.MustCompile(`(?i)<br\s*/?>`)

var collapse = regexp.MustCompile(`\s+`)

// Nothing inside these is readable text.
var skip = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true, "svg": true,
	"head": true, "title": true,
}

// Tags that start a new block on the page. An element containing none of them
// is the smallest thing a reader sees as one run of words, which is the unit
// worth keeping: any larger and the same words repeat once per layer, any
// smaller and a sentence splits wherever a designer coloured one word.
var block = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "details": true,
	"div": true, "dl": true, "dd": true, "dt": true, "fieldset": true, "figcaption": true,
	"figure": true, "footer": true, "form": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "header": true, "hr": true, "li": true,
	"main": true, "nav": true, "ol": true, "p": true, "pre": true, "section": true,
	"table": true, "tbody": true, "td": true, "th": true, "thead": true, "tr": true, "ul": true,
}

// Clean turns a downloaded page into the text a reader sees.
//
// Three things here are not obvious, and each one produced a knowledge base
// that looked healthy while answering wrongly. Navigation and footers repeat
// on every page; left in, they land in every piece and make unrelated pages
// score high for whatever they advertise. Site builders hide conditional and
// unpublished blocks with a class and leave the text in the markup; a
// downloader reads them as ordinary content. And parts of a page are often
// assembled in the visitor's browser, which a downloader never runs, so those
// parts arrive empty while the page around them looks normal; MissingStrings
// is the only way to notice.
//
// The page is read with a real HTML parser, never with a pattern. Surveyed: a
// pattern-based stripper leaks attribute contents, including script source,
// into the text, because a closing angle bracket inside an attribute value
// ends the match early.
func Clean(raw []byte, stripSelectors []string) (Cleaned, error) {
	// A line break carries no text, so the lines either side of it run
	// together. Addresses are written as lines, and a street number glued to
	// a city is neither readable nor findable. Replacing the tag before
	// parsing is the simplest way to keep the gap.
	raw = lineBreak.ReplaceAll(raw, []byte(" "))

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return Cleaned{}, fmt.Errorf("parse the page: %w", err)
	}
	title := squash(doc.Find("title").First().Text())

	// Remove anything unreadable before reading a word. A container holding
	// only a stylesheet otherwise looks like plain text, and its rules land in
	// the page as though a visitor could read them.
	for tag := range skip {
		doc.Find(tag).Remove()
	}
	for _, sel := range stripSelectors {
		doc.Find(sel).Remove()
	}

	var lines []string
	doc.Find("body *").Each(func(_ int, s *goquery.Selection) {
		node := s.Get(0)
		if node == nil || node.Type != html.ElementNode {
			return
		}
		tag := node.Data
		if skip[tag] {
			return
		}
		if prefix, ok := headings[tag]; ok {
			if text := squash(s.Text()); text != "" {
				lines = append(lines, prefix+" "+text)
			}
			return
		}
		// Keep an element only when nothing inside it starts a new block.
		// That is the smallest run of words a reader sees as one thing.
		if holdsBlock(node) {
			return
		}
		// An ancestor with no block inside it already carried these words.
		// Take them once, at the outermost element that holds the whole run.
		if p := node.Parent; p != nil && p.Type == html.ElementNode && !skip[p.Data] && !holdsBlock(p) {
			return
		}
		text := squash(s.Text())
		if text == "" {
			return
		}
		if tag == "li" {
			text = "- " + text
		}
		lines = append(lines, text)
	})

	markdown := strings.TrimSpace(strings.Join(lines, "\n\n"))
	sum := sha256.Sum256([]byte(markdown))
	return Cleaned{Title: title, Markdown: markdown, ContentHash: hex.EncodeToString(sum[:])}, nil
}

// holdsBlock reports whether anything inside the element starts a new block.
// It walks the element's own descendants and nothing after it, so a heading
// does not appear to contain the rest of the page.
func holdsBlock(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (block[c.Data] || holdsBlock(c)) {
			return true
		}
	}
	return false
}

// MissingStrings reports which expected phrases never arrived. A non-empty
// result means the download is incomplete, not that the page is wrong.
// Publishing anyway produces a knowledge base with holes that look identical
// to a working one until somebody asks the wrong question.
func MissingStrings(markdown string, required []string) []string {
	lowered := strings.ToLower(markdown)
	var missing []string
	for _, s := range required {
		if !strings.Contains(lowered, strings.ToLower(s)) {
			missing = append(missing, s)
		}
	}
	return missing
}

func squash(s string) string {
	return strings.TrimSpace(collapse.ReplaceAllString(s, " "))
}
