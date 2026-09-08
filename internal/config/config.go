// Package config loads siteconfig.toml, the only file permitted to name the
// company, its address, or its page structure.
//
// Decoding is strict: a key in the file that no field claims is an error. A
// misspelled key that silently does nothing is the failure mode this guards
// against, and it is expensive to diagnose because everything still runs.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// MaxVectorDimensions is the widest vector the database can store, measured
// rather than assumed: VECTOR_MAX_DIMENSION() reports this value, and the
// embedding function exposes no way to request a narrower vector, so a corpus
// necessarily sits exactly at this ceiling.
//
// This is the single source of truth for the number. The schema migration and
// siteconfig.toml both carry it too, because SQL and TOML cannot read a Go
// constant; both name this constant in a comment, and a live-tier test asserts
// the declared column width still matches it.
const MaxVectorDimensions = 3072

// Config is the whole of siteconfig.toml.
//
// Parse checks every field whose absence or wrong value would break the
// system at runtime, and reports all of them at once. Fields it does not check
// are marked in their own comments as either documentation, which no code path
// reads, or optional, where an empty value is a legitimate choice.
type Config struct {
	Site      Site      `toml:"site"`
	Bot       Bot       `toml:"bot"`
	Messages  Messages  `toml:"messages"`
	Models    Models    `toml:"models"`
	Retrieval Retrieval `toml:"retrieval"`
	Chunking  Chunking  `toml:"chunking"`
	Crawl     Crawl     `toml:"crawl"`
	Admin     Admin     `toml:"admin"`
	Chat      Chat      `toml:"chat"`
}

type Site struct {
	Name       string `toml:"name"`
	URL        string `toml:"url"`
	Sitemap    string `toml:"sitemap"`
	ContactURL string `toml:"contact_url"`
}

type Bot struct {
	Persona string `toml:"persona"`
	Tone    string `toml:"tone"`
	// Topics is documentation of expected coverage. No code path reads it, so
	// it is not validated.
	Topics []string `toml:"topics"`
	// Facts are company-vouched statements injected into the prompt. Optional:
	// empty means the pages speak for themselves, so it is not validated.
	Facts []string `toml:"facts"`
}

type Messages struct {
	Greeting  string `toml:"greeting"`
	Declined  string `toml:"declined"`
	Halted    string `toml:"halted"`
	Restarted string `toml:"restarted"`
	Error     string `toml:"error"`
}

// Models names vsql_ai providers and models. The provider strings are passed
// verbatim as the first argument to ai_prompt and ai_embedding, so a change
// here must be a value that extension recognises.
type Models struct {
	ChatProvider      string `toml:"chat_provider"`
	ChatModel         string `toml:"chat_model"`
	EmbeddingProvider string `toml:"embedding_provider"`
	EmbeddingModel    string `toml:"embedding_model"`
}

type Retrieval struct {
	// Dimensions must equal what the embedding model actually returns and what
	// the SVECTOR column is declared as. ai_embedding exposes no options
	// argument, so a narrower vector cannot be requested.
	Dimensions int `toml:"dimensions"`
	// Distance and Index are documentation. The distance function and the
	// absence of an index are both fixed by the extension, so no code path
	// reads either, and neither is validated.
	Distance string `toml:"distance"`
	Index    string `toml:"index"`
	TopK     int    `toml:"top_k"`
	// SimilarityThreshold is inherited and untuned. See DEFERRED.md.
	SimilarityThreshold float64 `toml:"similarity_threshold"`
}

type Chunking struct {
	SplitOn   []string `toml:"split_on"`
	MinTokens int      `toml:"min_tokens"`
	MaxTokens int      `toml:"max_tokens"`
	// TokensPerWord converts a word count into an approximate token count.
	// Biased to over-count on purpose: the embedding provider rejects input
	// above ProviderTokenCap outright, so under-counting produces a refusal.
	TokensPerWord float64 `toml:"tokens_per_word"`
	// ProviderTokenCap is the embedding model's hard input limit.
	ProviderTokenCap int     `toml:"provider_token_cap"`
	OverlapRatio     float64 `toml:"overlap_ratio"`
}

type Crawl struct {
	// Optional. Both values are meaningful, so there is nothing to validate.
	FollowRedirects bool   `toml:"follow_redirects"`
	Concurrency     int    `toml:"concurrency"`
	TimeoutSeconds  int    `toml:"timeout_seconds"`
	UserAgent       string `toml:"user_agent"`
	// RobotsDisallowed mirrors what the site's robots.txt refuses. The sitemap
	// may list these anyway; sitemap membership is not a permission grant.
	// The three exclusion lists are optional: an empty list excludes nothing,
	// which is a legitimate configuration, so none of them is validated.
	RobotsDisallowed []string `toml:"robots_disallowed"`
	DefaultExclude   []string `toml:"default_exclude"`
	// DefaultExcludeGroups drops whole path groups, such as pages about people
	// other than the site owner, which no content rule can detect.
	DefaultExcludeGroups []string `toml:"default_exclude_groups"`
	// MinBodyWords drops documents that are mostly site chrome once stripped.
	MinBodyWords int `toml:"min_body_words"`
	// Optional. An empty StripSelectors strips nothing and an empty
	// RequiredStrings asserts nothing; both are legitimate.
	StripSelectors  []string `toml:"strip_selectors"`
	RequiredStrings []string `toml:"required_strings"`
}

type Admin struct {
	SessionCookieName string `toml:"session_cookie_name"`
	SessionMaxAge     int    `toml:"session_max_age"`
}

type Chat struct {
	StatusPollSeconds int `toml:"status_poll_seconds"`
}

// Load reads and validates a configuration file.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(raw, path)
}

// Parse decodes configuration bytes. It is separate from Load so tests can
// supply their own document without touching the filesystem.
func Parse(raw []byte, name string) (Config, error) {
	var c Config
	dec := toml.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&c); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return Config{}, fmt.Errorf("%s has keys nothing reads:\n%s", name, strict.String())
		}
		return Config{}, fmt.Errorf("parse %s: %w", name, err)
	}
	if err := c.validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", name, err)
	}
	return c, nil
}

// validate reports every problem it finds rather than the first, so one run
// fixes one round of edits. It is unexported because Parse is the only way in:
// a Config that exists has already been validated.
func (c Config) validate() error {
	var bad []string
	require := func(cond bool, msg string) {
		if !cond {
			bad = append(bad, msg)
		}
	}

	require(c.Site.Name != "", "site.name is empty")
	require(c.Site.URL != "", "site.url is empty")
	require(c.Site.Sitemap != "", "site.sitemap is empty")
	require(c.Site.ContactURL != "", "site.contact_url is empty")

	require(c.Bot.Persona != "", "bot.persona is empty")
	require(c.Bot.Tone != "", "bot.tone is empty")

	// Every message is shown to a visitor. An empty one is a blank screen at
	// the moment the system is least able to explain itself.
	require(c.Messages.Greeting != "", "messages.greeting is empty")
	require(c.Messages.Declined != "", "messages.declined is empty")
	require(c.Messages.Halted != "", "messages.halted is empty")
	require(c.Messages.Restarted != "", "messages.restarted is empty")
	require(c.Messages.Error != "", "messages.error is empty")

	require(c.Models.ChatProvider != "", "models.chat_provider is empty")
	require(c.Models.ChatModel != "", "models.chat_model is empty")
	require(c.Models.EmbeddingProvider != "", "models.embedding_provider is empty")
	require(c.Models.EmbeddingModel != "", "models.embedding_model is empty")

	// A narrower vector is as unusable as a wider one: the embedding column is
	// declared at exactly this width, and the embedding function exposes no
	// options argument, so it always returns this many elements.
	require(c.Retrieval.Dimensions == MaxVectorDimensions,
		fmt.Sprintf("retrieval.dimensions must be %d, which is both the vector maximum and the only width the embedding model returns",
			MaxVectorDimensions))
	require(c.Retrieval.TopK > 0, "retrieval.top_k must be positive")
	require(c.Retrieval.SimilarityThreshold >= 0 && c.Retrieval.SimilarityThreshold <= 1,
		"retrieval.similarity_threshold must be between 0 and 1")

	require(len(c.Chunking.SplitOn) > 0, "chunking.split_on is empty, so nothing would be split")
	require(c.Chunking.MinTokens > 0, "chunking.min_tokens must be positive")
	require(c.Chunking.MaxTokens > c.Chunking.MinTokens,
		"chunking.max_tokens must exceed chunking.min_tokens")
	require(c.Chunking.TokensPerWord > 0, "chunking.tokens_per_word must be positive")
	require(c.Chunking.ProviderTokenCap > 0, "chunking.provider_token_cap must be positive")
	require(c.Chunking.MaxTokens <= c.Chunking.ProviderTokenCap,
		"chunking.max_tokens exceeds chunking.provider_token_cap, so the provider would refuse a full chunk")
	require(c.Chunking.OverlapRatio >= 0 && c.Chunking.OverlapRatio < 1,
		"chunking.overlap_ratio must be at least 0 and below 1")

	require(c.Crawl.Concurrency > 0, "crawl.concurrency must be positive")
	require(c.Crawl.TimeoutSeconds > 0, "crawl.timeout_seconds must be positive")
	require(c.Crawl.UserAgent != "", "crawl.user_agent is empty")
	require(c.Crawl.MinBodyWords >= 0, "crawl.min_body_words cannot be negative")

	require(c.Admin.SessionCookieName != "", "admin.session_cookie_name is empty")
	require(c.Admin.SessionMaxAge > 0, "admin.session_max_age must be positive")
	require(c.Chat.StatusPollSeconds > 0, "chat.status_poll_seconds must be positive")

	if len(bad) > 0 {
		return errors.New(strings.Join(bad, "; "))
	}
	return nil
}
