package config_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/carlisia/erdac-village/internal/config"
)

// valid is a complete, deliberately invented configuration. Tests never read
// the real siteconfig.toml: a test that depends on live configuration fails
// for reasons that have nothing to do with the code under test.
const valid = `
[site]
name        = "Example Consulting"
url         = "https://example.test/"
sitemap     = "https://example.test/sitemap.xml"
contact_url = "https://example.test/contact"

[bot]
persona = "a careful assistant"
tone    = "plain"
topics  = ["what we do"]
facts   = []

[messages]
greeting  = "Ask away."
declined  = "Not on this site."
halted    = "Paused."
restarted = "Cleared."
error     = "Something went wrong."

[models]
chat_provider      = "anthropic"
chat_model         = "test-chat"
embedding_provider = "google"
embedding_model    = "test-embed"

[retrieval]
dimensions           = 3072
distance             = "cosine"
index                = "none"
top_k                = 5
similarity_threshold = 0.25

[chunking]
split_on           = ["h2"]
min_tokens         = 100
max_tokens         = 800
tokens_per_word    = 1.4
provider_token_cap = 2048
overlap_ratio      = 0.15

[crawl]
follow_redirects       = true
concurrency            = 4
timeout_seconds        = 20
user_agent             = "test-agent"
robots_disallowed      = ["/tags/*"]
default_exclude        = ["/404"]
default_exclude_groups = ["/people/*"]
min_body_words         = 40
strip_selectors        = ["nav"]
required_strings       = []

[admin]
session_cookie_name = "test_admin"
session_max_age     = 43200

[chat]
status_poll_seconds = 4
`

func parse(t *testing.T, body string) (config.Config, error) {
	t.Helper()
	return config.Parse([]byte(body), "test.toml")
}

// replaceOnce edits the fixture and fails if the text it was told to replace
// is not present exactly once. Without this, reformatting the fixture would
// turn a replacement into a no-op, and the test would keep passing while
// asserting nothing about the case it names.
func replaceOnce(t *testing.T, body, old, new string) string {
	t.Helper()
	if n := strings.Count(body, old); n != 1 {
		t.Fatalf("fixture contains %q %d times, want exactly 1", old, n)
	}
	return strings.Replace(body, old, new, 1)
}

func TestLoadsAValidDocument(t *testing.T) {
	c, err := parse(t, valid)
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if c.Site.Name != "Example Consulting" {
		t.Errorf("site.name = %q", c.Site.Name)
	}
	if c.Retrieval.Dimensions != 3072 {
		t.Errorf("retrieval.dimensions = %d, want 3072", c.Retrieval.Dimensions)
	}
	if len(c.Bot.Facts) != 0 {
		t.Errorf("bot.facts = %v, want empty", c.Bot.Facts)
	}
}

// The reason for choosing go-toml/v2 over the alternative: a key nothing reads
// is a typo, and a typo that silently does nothing is expensive to find.
func TestAnUnreadKeyIsAnError(t *testing.T) {
	body := replaceOnce(t, valid,
		"top_k                = 5",
		"top_k                = 5\ntop_kk               = 9")
	_, err := parse(t, body)
	if err == nil {
		t.Fatal("a key nothing reads was accepted")
	}
	if !strings.Contains(err.Error(), "top_kk") {
		t.Errorf("error does not name the offending key: %v", err)
	}
}

// Measured behaviour of go-toml/v2, pinned here because it surprised us and
// it weakens the strictness above: key matching ignores case, so a key that
// differs from a field only in capitalisation is NOT reported as unknown. It
// binds to the field instead.
func TestKeyMatchingIgnoresCase(t *testing.T) {
	body := replaceOnce(t, valid, "top_k                = 5", "TOP_K                = 9")
	c, err := parse(t, body)
	if err != nil {
		t.Fatalf("a case-variant key was rejected: %v", err)
	}
	if c.Retrieval.TopK != 9 {
		t.Errorf("retrieval.top_k = %d, want 9 from the case-variant key", c.Retrieval.TopK)
	}
}

func TestValidationReportsEveryProblemAtOnce(t *testing.T) {
	body := replaceOnce(t, valid, "top_k                = 5", "top_k                = 0")
	body = replaceOnce(t, body, `name        = "Example Consulting"`, `name        = ""`)
	_, err := parse(t, body)
	if err == nil {
		t.Fatal("invalid config accepted")
	}
	for _, want := range []string{"site.name", "retrieval.top_k"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

// SVECTOR's declared maximum. Measured, not assumed: VECTOR_MAX_DIMENSION()
// returns 3072 and ai_embedding offers no way to request a narrower vector.
func TestDimensionsAboveTheVectorMaximumAreRejected(t *testing.T) {
	over := strconv.Itoa(config.MaxVectorDimensions + 1)
	body := replaceOnce(t, valid, "dimensions           = 3072", "dimensions           = "+over)
	_, err := parse(t, body)
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(config.MaxVectorDimensions)) {
		t.Fatalf("want a complaint naming the %d maximum, got %v", config.MaxVectorDimensions, err)
	}
}

// A chunk larger than the provider's hard input limit is refused outright, so
// the configuration must not be able to describe one.
func TestMaxTokensAboveTheProviderCapIsRejected(t *testing.T) {
	body := replaceOnce(t, valid, "max_tokens         = 800", "max_tokens         = 4096")
	_, err := parse(t, body)
	if err == nil || !strings.Contains(err.Error(), "provider_token_cap") {
		t.Fatalf("want a complaint about the provider cap, got %v", err)
	}
}

// The doc comment on Config promises that every field whose absence would
// break the system is checked. These are the ones a previous revision missed.
func TestEmptyVisitorMessagesAreRejected(t *testing.T) {
	body := replaceOnce(t, valid, `declined  = "Not on this site."`, `declined  = ""`)
	_, err := parse(t, body)
	if err == nil || !strings.Contains(err.Error(), "messages.declined") {
		t.Fatalf("want a complaint about the empty declined message, got %v", err)
	}
}

func TestEmptyPersonaIsRejected(t *testing.T) {
	body := replaceOnce(t, valid, `persona = "a careful assistant"`, `persona = ""`)
	_, err := parse(t, body)
	if err == nil || !strings.Contains(err.Error(), "bot.persona") {
		t.Fatalf("want a complaint about the empty persona, got %v", err)
	}
}

func TestEmptySplitOnIsRejected(t *testing.T) {
	body := replaceOnce(t, valid, `split_on           = ["h2"]`, `split_on           = []`)
	_, err := parse(t, body)
	if err == nil || !strings.Contains(err.Error(), "chunking.split_on") {
		t.Fatalf("want a complaint about an empty split_on, got %v", err)
	}
}

// A narrower vector is as unusable as a wider one, because the column is
// declared at one width and the embedding model returns exactly that many
// elements with no way to ask for fewer.
func TestDimensionsBelowTheColumnWidthAreRejected(t *testing.T) {
	under := strconv.Itoa(config.MaxVectorDimensions / 2)
	body := replaceOnce(t, valid, "dimensions           = 3072", "dimensions           = "+under)
	_, err := parse(t, body)
	if err == nil || !strings.Contains(err.Error(), "retrieval.dimensions") {
		t.Fatalf("want a complaint about the narrow vector, got %v", err)
	}
}
