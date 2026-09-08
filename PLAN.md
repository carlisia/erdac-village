# Erdac Village

A port of [Erdac](../erdac) to Go, replacing Postgres and pgvector with a locally built, self-hosted [VillageSQL](https://github.com/villagesql). Local only. The crawl target is named in `siteconfig.toml`.

Erdac is a grounded chatbot with an admin portal: it crawls one company website, embeds the pages, and answers visitor questions only from what it crawled, refusing when the pages do not cover the question. An admin reviews every fetched page before it goes live.

## Why this repo exists

The point is not to have two chatbots. It is to build the same product against a database that does not support several of the things the original relies on, and to record every place the two diverge and why. Rough edges are the deliverable, not an obstacle.

Two rules govern every decision here.

**Lean on the VillageSQL ecosystem wherever it reaches.** Embedding, similarity search and generation all execute inside `mysqld` via `vsql_ai` and `vsql_vector`, even where doing that in Go would be faster, more reliable, or better instrumented.

**When the stack blocks a feature, pin it rather than route around it.** No fallback implementations. The gap goes in [PORTING.md](PORTING.md) with what was lost, and in [CONTRIBUTIONS.md](CONTRIBUTIONS.md) with what would close it upstream. The one exception is a workaround VillageSQL's own documentation prescribes -- a generated column standing in for a partial unique index is the ecosystem's answer, not a bypass, so it gets written.

## Stack

| Concern | Choice | Decision |
|---|---|---|
| Language | Go 1.25 | given |
| Module | `github.com/carlisia/erdac-village` | Q19 |
| Database | VillageSQL, prebuilt dev-server under `~/.villagesql`, native macOS arm64 | Q5b, Q15 |
| Extensions | `vsql_vector` pinned at `ec2282c`, `vsql_mcp` at `0.0.6`, both built against SDK 0.0.6 | Q15 |
| Vector search | `SVECTOR`, `COSINE_DISTANCE`, sequential scan | Q8b |
| Embeddings | `ai_embedding('google', 'gemini-embedding-001', ...)`, 3072 dimensions | Q9, Q17, Q30 |
| Generation | `ai_prompt('anthropic', ...)` | Q17, Q18 |
| Driver | `go-sql-driver/mysql` | Q22 |
| Query layer | `sqlx` (sqlc structurally disqualified) | Q23 |
| HTTP | stdlib `net/http` | Q33 |
| Config | `pelletier/go-toml/v2`, strict via `DisallowUnknownFields()` | Q33 |
| HTML | `goquery` | Q24 |
| robots.txt | `jimsmart/grobotstxt` | Q24 |
| sitemap.xml | `encoding/xml` | Q24 |
| Telemetry | `openinference-semantic-conventions` + OTel Go, OTLP/HTTP to Phoenix | Q34 |
| Tests | fakes and `go-sqlmock` in the fast tier; live server behind `TEST_MYSQL_DSN` | Q21, Q25 |
| Frontend | Erdac's `public/` copied verbatim | Q3 |

## What is mirrored

Everything Erdac does, subject to the pin rule. The full inventory is 11 HTTP routes, 5 tables, the candidate/published/superseded review workflow, the ingest pipeline with its canary gate and skip logic, the answering pipeline with its similarity cutoff and `[[NO_ANSWER]]` sentinel, the admin portal's eighteen affordances, and 214 tests.

Structure is not mirrored. This is idiomatic Go, not a transliteration of the Python package layout. What is preserved from that layout is the pair of seams: where pages come from, and where results go. Those are what let a test run without touching the world (Q1). They are boundaries rather than types -- each has one concrete implementation, and consumers declare the handful of methods they call.

## What is deliberately not built

Erdac's `infra/` is a twelve-stage deployment wizard, DNS setup, Vercel linking, env sync, verification and teardown. None of it has an analogue here, because there is nothing to deploy to (Q4).

Three properties those scripts existed to guarantee do survive in local form: secrets never live in the repository, a preflight says whether this machine can actually run the thing, and a teardown removes what was installed.

## Build order

Every unverified assumption in this plan lies on one narrow path. Feature-order construction reaches them last, with a great deal of code already resting on them, so the first thing built is a vertical spike that touches all of them and nothing else (Q35).

The spike, in order. No HTTP, no crawl, no schema, no tests.

**All seven steps are done as of 2026-09-07.** The spike proved the path end to end: extensions loaded, three sentences embedded at 3072 dimensions, similarity search ranking correctly, and a grounded answer plus a correct refusal from a flattened prompt.

1. Install the prebuilt dev server under `~/.villagesql`.
2. Build and load `vsql_vector` and `vsql_mcp` against the extension SDK.
3. Create one `SVECTOR(3072)` table and select from it with a bound parameter present.
4. Confirm the query-vector join runs against hand-written vectors, before any provider is involved.
5. Embed three sentences with `ai_embedding`, and confirm the returned width really is 3072.
6. Search them with the query-vector join, since a literal vector argument is rejected.
7. Answer one question with `ai_prompt` using a flattened prompt, and see whether the sentinel fires.

Each step kills an assumption. `PORTING.md` gets its first entries from that spike, from measurement rather than from research.

After the spike holds: schema and the review workflow, then ingest, then answering, then the HTTP surface, then the portal, then the test suite brought up to parity.

## Assumptions, and what measurement did to them

All four are resolved. The spike ran end to end on 2026-09-07.

1. **Dev-ABI protocol match. Confirmed broken.** `vsql-vector` has no releases and its `main` does not build against SDK 0.0.6, on a Key API change committed the day after the SDK was cut. Pinned at `ec2282c`. The pin must be re-chosen by hand on any SDK upgrade.

2. **Driver field-type handling. Disproven.** `go-sql-driver/mysql` reads an `SVECTOR` column fine, returning `[]byte` under both protocols.

3. **The query-vector join. Disproven, and better than expected.** `SVECTOR::FROM_STRING` accepts computed strings, so a question's embedding can be passed inline and search performs no write at all. The documented join workaround is unnecessary.

4. **The sentinel survives flattening. Confirmed.** With no system role, a covered question produced a grounded answer and an uncovered one produced exactly `[[NO_ANSWER]]`. A two-case result, not yet a refusal-rate measurement.

5. **The vector fits. Confirmed, exactly.** `gemini-embedding-001` returns 3072 elements, measured, and `VECTOR_MAX_DIMENSION()` is 3072. No headroom.

A fifth thing was found that no assumption anticipated: **the two extensions do not compose.** An embedding produced by `ai_embedding` cannot be written into an `SVECTOR` column from SQL at all, so embeddings round-trip through Go as string literals. See `PORTING.md`.

## Two constants inherited but void

`retrieval.similarity_threshold = 0.25` was tuned against 28 questions about a different website using a different embedding model. Both inputs changed, so the number carries no evidence. It ships marked untuned; new fixtures get drafted from the crawl survey and reviewed before the value is trusted (Q31).

`chunking` sizes of 100 / 800 / 0.15 were measured in `cl100k_base` tokens. Google's models do not use that tokenizer and Go has no maintained implementation of the one they do use, so counting moves to a word-count approximation whose divisor is calibrated once against Google's `countTokens` endpoint and hard-coded with its derivation (Q32, Q36). The 800 ceiling sits under `gemini-embedding-001`'s 2048-token input limit, so re-tuning is expected to mean confirming the numbers rather than changing them (Q37). That limit is a hard provider cap, not a soft target: a chunk over it is rejected outright.

## The crawl target

The crawl target publishes a sitemap. A large minority of its entries are paths its own robots.txt disallows, and a further third are bare alias stubs that redirect to canonical paths, sometimes as case-variant pairs. Roughly half the sitemap is worth fetching. The exclusion rules and the body-length floor that handle both are in `siteconfig.toml`.

Two hazards are known in advance. One path group holds pages about people other than the site owner, which must not be blended into answers about who the site is about. Every page carries the same header, footer, sidebar and graph-view chrome, so a naive crawl produces many documents that are mostly boilerplate.

The rule that no company name, address, or page structure appears outside `siteconfig.toml` is kept even though the target is now the author's own site (Q7). Test fixtures stay invented, because a test that depends on a live website is not a test.
