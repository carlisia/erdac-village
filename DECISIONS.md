# Decisions

Every decision the user made, newest last. The reasoning behind each one is here; `PLAN.md` holds the resulting shape.

These entries were written in one sitting at the close of the planning session rather than as each decision landed, because the repository did not exist while the decisions were being made. Everything after this backfill is appended as it happens.

The decisions came in seven rounds of a structured interview. Round timestamps are the close of the planning session, not the moment each answer was given; the ordering within a round is the order the questions were asked.

## 2026-09-07 19:32 PDT - Mirror behaviour, not structure

**Decision:** Port Erdac's observable behaviour to idiomatic Go. Do not reproduce its Python package layout. Keep the two seams: a `Store` interface for where pages go and a `Source` interface for where they come from.

**Superseded in part.** The seams are kept; describing each as a single interface is not. See "Store boundary: one concrete type, consumer-declared interfaces" below. This entry is left as written because it records what was decided at the time.

**Why:** A Python-shaped Go project reads as a translation exercise and invites a reader to judge the translation rather than the design. The seams are kept because they are what lets a test run without touching a database or a network, and that property is worth more than the file layout.

**Alternatives rejected:** File-for-file mirroring, which would make the two repositories diffable. Rejected because the value of this repository is showing the same product against a different stack, not showing the same code in a different language.

**Recorded by:** grill-with-docs

## 2026-09-07 19:32 PDT - Mirror the review workflow whatever it costs

**Decision:** Reproduce Erdac's two-versions-per-address model, where one address can hold a live row and a pending row at the same time, using whatever MySQL mechanism is required.

**Why:** It is what makes "nothing goes live until an admin reviews it" work while the bot keeps answering from the previous version. Postgres enforces it with partial unique indexes, which MySQL does not have, so this is the case that most tests the instruction to mirror all features.

**Alternatives rejected:** One row per address, which would mean a page under review stops answering. Rejected because it is a regression that was already found and fixed once in Erdac, where a refresh took the live site from 68 pages to 55. Dropping review entirely was also rejected, as it is the product's main safety property.

**Recorded by:** grill-with-docs

## 2026-09-07 19:32 PDT - Copy the frontend verbatim

**Decision:** Copy Erdac's `public/` directory unchanged, including its README.

**Why:** It is already backend-agnostic. Three static HTML files driven by `data-erdac-*` attributes talking to JSON endpoints, with no build step and no language coupling. Re-doing it would spend effort on the least interesting surface in the project.

**Alternatives rejected:** Rewriting the frontend. Rejected for the reason above, and because the shared markup contract between the two repositories is itself worth pointing at.

**Recorded by:** grill-with-docs

## 2026-09-07 19:32 PDT - Drop deployment, keep what it protected

**Decision:** Do not port `infra/`: no wizard, no DNS, no Vercel, no environment sync, no remote verification. Keep three properties in local form: secrets never live in the repository, a preflight reports whether this machine can run the system, and a teardown removes what was installed.

**Why:** Local-only means there is nothing to deploy to, so the scripts have no analogue. The three properties are why those scripts existed and they still matter.

**Alternatives rejected:** Porting the deployment machinery anyway, which would be building for a target that does not exist. Silently omitting it was also rejected; the omission is recorded in `PLAN.md` rather than left for a reader to notice.

**Recorded by:** grill-with-docs

## 2026-09-07 19:32 PDT - Run VillageSQL natively, not in a container

**Decision:** Install the prebuilt dev-server tarball under `~/.villagesql` and run it natively on macOS arm64. Clone and build the extensions locally against the extension SDK.

**Why:** A `.veb` extension bundle built on macOS contains a Mach-O dylib, and the official Docker image is Ubuntu, so a host-built extension cannot load in the container regardless of how it is mounted. The native path also ships `mysql-test/`, without which the extensions' own test suites cannot run, and it pairs the SDK and server versions automatically.

**Alternatives rejected:** The container, which was the user's initial preference, and which would require either building extensions inside it or grafting a Rust toolchain into someone else's image. Also rejected: running the container with no extensions at all, which would remove the whole point.

**Recorded by:** grill-with-docs

## 2026-09-07 19:32 PDT - Carry the documentation set, and add two files

**Decision:** Carry `CONTEXT.md`, `DECISIONS.md`, `DEFERRED.md`, `EXCEPTIONS.md`, `PLAN.md` and `docs/adr/` across from Erdac. Add `PORTING.md` for divergences and `CONTRIBUTIONS.md` for upstream gaps.

**Why:** Divergences are the output this repository exists to produce, and mixing them into `DECISIONS.md` would bury them among ordinary decisions. Splitting `CONTRIBUTIONS.md` from `PORTING.md` separates what this project lost from what the upstream project could fix.

**Alternatives rejected:** One combined notes file. Rejected because the outward-facing material is the part most worth handing to someone else, and it should not require filtering out project-internal notes first.

**Recorded by:** grill-with-docs

## 2026-09-07 19:32 PDT - Keep the no-company-name rule

**Decision:** Company name, brand, domain, page structure and page counts stay in `siteconfig.toml` only, even though the crawl target is now the author's own site.

**Why:** It costs nothing, it keeps the two repositories comparable, and it is the property that makes either one a credible template rather than a one-off.

**Alternatives rejected:** Relaxing the rule since the site is the author's. Rejected because the rule's value is structural, not privacy-driven.

**Recorded by:** grill-with-docs

## 2026-09-07 19:32 PDT - Lean on the VillageSQL ecosystem, and pin what it blocks

**Decision:** Use VillageSQL and its extensions wherever they reach, even where a Go implementation would be better. When the stack blocks a feature, record it in `PORTING.md` and `CONTRIBUTIONS.md` rather than implementing an alternative. One exception: a workaround VillageSQL's own documentation prescribes is the ecosystem's answer and gets written.

**Why:** The user has an interview with VillageSQL. Every rough edge encountered is preparation, and performance is explicitly not a constraint. The exception exists because without it the review workflow would have no uniqueness enforcement at all, since the generated-column substitute for a partial unique index is a vendor-documented idiom.

**Alternatives rejected:** Implementing Go fallbacks for blocked features, which would produce a working system that demonstrates nothing about the database.

**Recorded by:** grill-with-docs

## 2026-09-07 19:32 PDT - Similarity search runs in the database

**Decision:** `vsql_vector` is the retrieval path. No cosine implementation in Go.

**Why:** Direct consequence of the ecosystem rule. Install the extension and measure what it actually does rather than reading its README and forming an opinion.

**Alternatives rejected:** Ranking in Go over embeddings fetched out of the database, which is what VillageSQL's own vector-embeddings guide recommends and what an earlier recommendation in this session argued for. Overruled by the ecosystem rule.

**Recorded by:** grill-with-docs

## 2026-09-07 19:32 PDT - Embeddings are generated inside the database

**Decision:** `ai_embedding()` generates every embedding. Go never calls an embedding API.

**Why:** Same rule. The function's documented behaviour includes returning NULL plus a warning instead of raising on failure, which is exactly the kind of edge worth meeting first-hand.

**Alternatives rejected:** A Go HTTP client, which would make failures loud and allow batching. Rejected under the ecosystem rule, with the silent-NULL hazard recorded in `PORTING.md` and `CLAUDE.md` instead.

**Recorded by:** grill-with-docs

## 2026-09-07 19:32 PDT - Prompt assembly stays in Go, the model call does not

**Decision:** Go builds the prompt text; SQL executes `ai_prompt` with it as a parameter.

**Why:** Erdac's prompt carries a contract asserted by 14 tests, covering the `[[NO_ANSWER]]` sentinel, the contact-link rule, the problem-versus-offer distinction and the injected facts block. That contract is checkable in Go and effectively not in `GROUP_CONCAT`. Embedding, search and generation all still happen in the database, so nothing is routed around it.

**Alternatives rejected:** Full-SQL retrieval-augmented generation in a single statement. Rejected because assembling a prompt in SQL exercises MySQL string functions rather than VillageSQL, so the rough edges it produces are not the ones worth collecting. Recorded in `PORTING.md` as an evaluated design.

**Recorded by:** grill-with-docs

## 2026-09-07 19:32 PDT - Module path

**Decision:** `module github.com/carlisia/erdac-village`.

**Why:** Go wants the real module path from the first commit, because changing it later rewrites every import in the repository. A module path is a name, not a commitment to publish.

**Alternatives rejected:** A placeholder path, which converts a free decision now into a repository-wide edit later.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - The query vector lives on the conversation-turn row

**Decision:** Store each question's embedding on the row that already logs that question, and join to it by id when searching.

**Why:** `vsql_vector` cannot accept a vector as a literal or bound parameter, because constant folding for parameterized custom types is unimplemented, so every search must join against a stored vector. Erdac already writes a log row per question for the unanswered-questions report, so the embedding lands on a row that has to exist anyway and introduces no new lifecycle.

**Alternatives rejected:** A dedicated scratch table, which adds rows that must then be cleaned up. A temporary table per connection, which couples the search to connection identity that a `database/sql` pool will take away without warning.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - Two test tiers

**Decision:** A fast tier using interface fakes and `go-sqlmock`, runnable with no database and no network. A second tier requiring the live server, skipped when `TEST_MYSQL_DSN` is unset. `check.sh` runs both and reports which one skipped.

**Why:** All 214 Erdac tests run offline because both seams have fakes behind them. Once retrieval, embedding and generation all live in SQL, a fake `Store` stops standing in for storage and starts standing in for the product, so a test against it proves little. Splitting keeps a suite that can always run, which is the suite that actually gets run.

**Alternatives rejected:** Live-server-only, because a suite that needs a hand-installed alpha database stops being run. Fakes-only, because the fakes would be asserting a model of VillageSQL rather than VillageSQL. Build tags for the live tier, because `//go:build integration` hides that code from `go vet` and gopls, and code the tooling cannot see rots while the fast suite stays green.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - No day-one check of the driver's custom-type handling

**Decision:** Do not verify up front whether `go-sql-driver/mysql` can read an `SVECTOR` column. Meet it during implementation.

**Why:** The design already avoids ever selecting an `SVECTOR` into Go, because embeddings are written server-side by `ai_embedding` and searches read a float distance plus chunk text. The verification was a confirmation step, not a safeguard.

**Alternatives rejected:** A day-one empirical check, which was the recommendation. The user overruled it. The risk is recorded as an unverified assumption in `PLAN.md`: the driver's binary protocol ends its type switch with an error on unknown field types rather than degrading to bytes, so if a vector ever does cross the wire the failure is hard.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - Query layer, HTTP router, and configuration library

**Decision:** `sqlx` for queries, stdlib `net/http` for routing, `pelletier/go-toml/v2` for configuration with strict decoding via `DisallowUnknownFields()`.

**Why:** `sqlx` never parses the SQL it sends, which matters with a vendor dialect, and it removes scan boilerplate. Stdlib routing covers all eleven routes since Go 1.22, and a short dependency list is easier to defend. `go-toml/v2` returns a `StrictMissingError` naming every unknown key, which catches a typo'd configuration key that would otherwise silently do nothing.

**Alternatives rejected:** sqlc, which is structurally disqualified: it parses DDL with Marino, a hard fork of the TiDB MySQL parser, and `SVECTOR(1536)` and `INSTALL EXTENSION` are not MySQL grammar, so it cannot build a catalog. `BurntSushi/toml` was recommended on the grounds that it alone offered strict validation; that reason was wrong, `go-toml/v2` has a stronger form of it, and the user's choice was correct.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - Crawling libraries

**Decision:** `goquery` for HTML extraction, `jimsmart/grobotstxt` for robots.txt, `encoding/xml` for sitemaps.

**Why:** `goquery` is actively maintained where both Go readability ports are not, and this project's extraction is selector-driven against one known site, so a general-purpose readability library solves a problem that does not exist here. `grobotstxt` ports Google's official C++ parser, which sharpens the existing robots.txt discussion carried over from Erdac. No maintained Go sitemap parser exists.

**Alternatives rejected:** `go-trafilatura` and `go-readability`, both slower-moving, one with no tagged releases at all. `temoto/robotstxt`, the more widely imported parser, effectively in maintenance since 2021.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - The chat endpoint returns one JSON payload

**Decision:** `/api/chat` returns a single JSON response. No streaming, and no fake streaming.

**Why:** `ai_prompt` is synchronous and returns the whole completion, so there is nothing to stream. Erdac's `readAnswer` auto-detects framed versus plain text from the first non-empty buffer, so the copied frontend is expected to accept this unchanged.

**Alternatives rejected:** Re-framing the single response as SSE chunks in Go, which would invent motion that does not exist and is an alternative implemented around a gap, which the pin rule forbids. The visible cost, a spinner for up to 30 seconds and a dead `streaming` body state, is recorded in `PORTING.md`.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - Flatten the message array, and measure what it costs

**Decision:** Flatten Erdac's system prompt, history turns and question into one labelled string. Keep all 14 prompt tests. Measure the refusal rate before and after against the question fixtures.

**Why:** `ai_prompt` takes one flat prompt and has no system role. The `[[NO_ANSWER]]` sentinel instruction lives in the system prompt, and the entire declined-versus-answered accounting depends on the model emitting it as the first token, so an instruction demoted out of the system role may be followed less reliably. Measuring turns that worry into a number.

**Alternatives rejected:** Flattening without measuring. Rejected because the sentinel is load-bearing and its degradation would be silent.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - Drop the token-count columns

**Decision:** Remove `query_log.prompt_tokens`, `query_log.completion_tokens` and the `llm.token_count.*` span attributes. Drop `chat_fallback` from configuration.

**Why:** `ai_prompt` returns only a string, so nothing can fill them, and it takes one model with no fallback concept.

**Alternatives rejected:** Keeping the columns and letting them sit NULL as a self-documenting reminder of the gap, which was the recommendation. The user overruled it. The consequence is that the `CONTRIBUTIONS.md` entry is now the only record that token accounting was ever possible, so that entry carries the full weight of the finding.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - Schema translations for arrays and timestamps

**Decision:** Replace `retrieved_chunk_ids bigint[]` and `scores real[]` with a `query_log_retrieval` child table holding log id, chunk id, score and rank. Use `DATETIME(6)` with UTC written and read explicitly.

**Why:** MySQL has no array types and VillageSQL's migration guide prescribes normalizing to a child table. The child table is arguably better than the arrays, which always relied on a positional correspondence nothing enforced. `DATETIME` over `TIMESTAMP` because `TIMESTAMP` converts on read by session timezone, and the admin portal displays fetch and publish times where a silent shift would be invisible and wrong.

**Alternatives rejected:** A JSON column holding id and score pairs, which is closer to the original shape but gives up the enforced correspondence.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - Build a vertical spike before anything else

**Decision:** First work is a spike touching the whole path and nothing else: install, load both extensions, create one `SVECTOR` table, embed three sentences, search them with the query-vector join, answer one question. No HTTP, no crawl, no schema, no tests.

**Why:** Four unverified assumptions all lie on that one path: the dev-ABI protocol match, the driver's field-type handling, the query-vector join workaround, and whether the sentinel survives prompt flattening. Feature-order construction reaches them last, with a great deal of code already resting on them. The spike also gives `PORTING.md` its first entries from measurement rather than research.

**Alternatives rejected:** Schema-first bottom-up construction, which reaches the riskiest assumption last. Repository scaffolding first, which is the part of the work with no unknowns in it.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - Similarity threshold ships untuned, with new fixtures

**Decision:** Carry `similarity_threshold` forward marked explicitly untuned. Draft a new question fixture set from the crawl survey of the target site, and have the user review the questions before the number is trusted.

**Why:** The inherited value was derived from 28 questions about a different website scored by a different embedding model. Both inputs changed, so it carries no evidence. The survey found a genuinely useful source of must-refuse questions: pages about other people that must not be blended into answers about the site owner.

**Alternatives rejected:** Carrying the number forward as a working default, which would leave an evidence-free value controlling whether the model is called at all.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - Approximate the token count, over-counting deliberately

**Decision:** Replace `tiktoken` with a word-count approximation whose divisor is calibrated once against the provider's token-counting endpoint and hard-coded with its derivation. Bias the divisor to over-count. Keep the chunk sizes at 100 / 800 / 0.15 and verify the largest produced chunk against the provider's real input limit.

**Why:** The embedding model is no longer an OpenAI model, so `cl100k_base` measures something unrelated to what gets embedded, and Go has no maintained implementation of the tokenizer that does apply. Chunk sizing needs a monotonic, stable measure rather than an exact one, since being consistently off by a fixed factor shifts every boundary equally. The bias to over-count exists because the provider rejects over-length input outright.

**Alternatives rejected:** Keeping `cl100k_base` deliberately for comparability, which was the recommendation; the user chose correctness of measurement over holding a variable fixed. A HuggingFace tokenizer port, which adds a dependency and a vocabulary file to keep in sync with the model. Calling the provider to count, which turns a pure function into a network call inside the chunk-splitting loop and breaks the offline test tier.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - Use the OpenInference Go package despite its pre-1.0 status

**Decision:** Depend on `openinference-semantic-conventions` for Go rather than copying its attribute-key constants.

**Why:** The attribute keys are the entire value of the package, and hand-copied constants are how one key ends up misspelled and silently absent from Phoenix with no error anywhere.

**Alternatives rejected:** Implementing against the language-neutral specification directly, which avoids a pre-1.0 dependency and an older pinned OpenTelemetry SDK, at the cost of hand-maintaining strings whose only job is to match exactly.

**Recorded by:** grill-with-docs (options presented via lavish)

## 2026-09-07 19:32 PDT - Embeddings come from Google, not from a local model

**Decision:** Use `ai_embedding('google', 'gemini-embedding-001', ...)`. Do not install Ollama. The API key is supplied later.

**Why:** Ollama was buying local execution for one stage of a pipeline that was already reaching the internet in another, since `ai_prompt` calls Anthropic from inside the database on every answer. It cost an install, a background service, a narrower vector, and the worst version of the tokenizer problem, in exchange for a property the system did not have.

**Alternatives rejected:** Ollama with `nomic-embed-text`, decided earlier in the session and reversed here. The reversal has a cost worth naming: `ai_embedding` has no options parameter, so the model's default 3072 dimensions is the only reachable width, which is exactly `SVECTOR`'s maximum and leaves no headroom, and the 1536 the model also supports cannot be requested.

**Recorded by:** none

## 2026-09-07 19:32 PDT - Do not start the spike yet

**Decision:** Planning work continues; no installation, no build, no execution of the spike.

**Why:** Stated by the user. Recorded because the plan names the spike as the next action, and a reader finding no spike should know it was held deliberately rather than overlooked.

**Alternatives rejected:** None offered.

**Recorded by:** none

## 2026-09-07 19:55 PDT - Q20 re-taken and confirmed unchanged

**Decision:** The question's embedding is still materialised into an `SVECTOR` column on the conversation-turn row and joined against. No change.

**Why:** The re-take was opened because a measurement appeared to show that a literal query vector works, which would have removed the reason for the design. Further measurement narrowed it: `COSINE_DISTANCE` accepts only a true constant or a custom-type column. A literal qualifies; a bound parameter, a user variable and `CONCAT(...)` do not. An embedding produced by `ai_embedding` is a function result, so it cannot be passed inline at all. The original design is the only one that works with an in-database embedding.

**Alternatives rejected:** Passing the embedding as a literal built in Go, which would require fetching it out of the database first and building roughly forty kilobytes of SQL text per question. Setting `interpolateParams=true` on the driver, which makes bound parameters work by interpolating them client-side, but changes escaping behaviour for every query in the system to fix one call site.

**Recorded by:** none

## 2026-09-07 19:55 PDT - MCP server configuration

**Decision:** `vsql_mcp` runs on 127.0.0.1:3100 with `require_auth` ON behind a generated bearer token, `allow_write` OFF, and a dedicated `mcp` account holding only `SELECT` on the working schema. Credentials live in `~/.villagesql/mcp-credentials.txt`, outside the repository.

**Why:** The extension's own README states there is no row ceiling on writes and that an unqualified `DELETE` empties the table, and that tool queries run under the `db_url` account's real grants regardless of tool-level guardrails. The grant is therefore the real boundary and the tool settings are defence in depth.

**Alternatives rejected:** Running as root with `allow_write` ON, which would make the guardrails cosmetic.

**Recorded by:** none

## 2026-09-07 20:20 PDT - Q20 open for a third time: search needs no write

**Decision:** Not yet taken. Recorded so the reversal is visible rather than silently applied.

**Why:** The design stores each question's embedding on its conversation-turn row and joins against it, because `COSINE_DISTANCE` rejects a computed second argument. Measurement found `SVECTOR::FROM_STRING`, which accepts computed strings, user variables and function results, and returns a value `COSINE_DISTANCE` takes. Search can therefore pass the embedding inline and perform no write, which means it can run against a read-only database user.

This decision has now moved twice on new evidence, so it is stated plainly rather than assumed: the write is no longer required, and whether to drop it is the user's call.

**Alternatives rejected:** None yet.

**Recorded by:** none

## 2026-09-07 20:20 PDT - Embeddings round-trip through Go as literals

**Decision:** Ingest issues `SELECT ai_embedding(...)`, receives the JSON array, and writes it back as a string literal in the following `INSERT`.

**Why:** There is no path from `ai_embedding`'s output into an `SVECTOR` column inside SQL. Six forms were tried and all fail; the explicit conversion returns a dimensionless `SVECTOR` that cannot be assigned to the dimensioned column a declaration requires. Two round trips and roughly 39 KB of SQL text per chunk is the only working route.

**Why this does not violate the pin rule:** The embedding is still generated by the database. Nothing was reimplemented in Go to route around a gap; the value is simply carried across a type boundary the database cannot cross by itself.

**Alternatives rejected:** Storing the embedding as `TEXT` and skipping `vsql_vector` entirely, which would abandon the extension the port exists to exercise.

**Recorded by:** none

## 2026-09-07 20:41 PDT - Q20 resolved: the stored query embedding is dropped

**Decision:** `query_log` carries no embedding column. A question's embedding is placed in a session variable, wrapped in `SVECTOR::FROM_STRING`, and passed inline to `COSINE_DISTANCE`. Search performs no write.

**Why:** Measured on 2026-09-07. `SVECTOR::FROM_STRING` accepts user variables, so the constraint that forced the write does not exist. Search can now run under a read-only database account, and a question no longer costs a row before it costs a query.

**Alternatives rejected:** Storing the embedding on the conversation-turn row, which was the decision through two earlier revisions of this plan and was correct under the evidence available at the time. It is retained in `PORTING.md` as the shape the vendor's README still prescribes.

**Recorded by:** none

## 2026-09-07 20:41 PDT - Specs are tracked in a local directory

**Decision:** Specifications live in `docs/specs/`, numbered, in this repository. No external issue tracker.

**Why:** Stated by the user. It also keeps the spec next to the decision record and the porting notes, so a reader has one place to look.

**Alternatives rejected:** A GitHub issue tracker, which would split the written record across two systems for a repository with no collaborators yet.

**Recorded by:** to-spec

## 2026-09-07 20:50 PDT - Store boundary: one concrete type, consumer-declared interfaces

**Decision:** A single concrete store type holds every database method. No package exports a wide interface. Each consumer declares the two to four methods it calls, beside the code that calls them. Transactions and database-shaped types never cross the boundary; failures are sentinel errors; every method takes a context first. The package is named for the noun and the type is not, so the reference reads as a database handle rather than as a stuttering repeated word.

**Why:** A ten-method interface is a Python Protocol translated across, which is the thing the port explicitly set out not to do. Consumers depending on methods they never call makes every fake churn when any caller gains a need. Sealing the transaction inside publish is what prevents a caller getting the retire-before-promote ordering wrong, which is a bug the original system shipped once.

Worth recording: the original did not have one store either. Its ingest module defines a four-method protocol, while its API layer separately substituted an entire database module. Two seams sharing one name.

**Alternatives rejected:** One wide interface, which is faithful to the original's vocabulary and wrong for Go. No interface at all, which is the most Go-ish reading of "do not abstract early" but deletes the fast test tier for everything touching storage, and a suite that needs a hand-installed alpha database is a suite that stops being run.

**Recorded by:** none

## 2026-09-07 21:00 PDT - Specifications moved under docs/

**Decision:** `specs/` becomes `docs/specs/`.

**Why:** Stated by the user. It also puts every written artifact except the top-level tracking files under one directory, so a reader browsing `docs/` finds the architecture decision records and the specifications together.

**Alternatives rejected:** None offered.

**Recorded by:** none

## 2026-09-07 21:13 PDT - Two-axis review: every finding fixed

**Decision:** Act on all fifteen findings from the standards and spec review, including the judgement calls.

**Why:** Two were defects the schema would have carried into every later feature. `CURRENT_TIMESTAMP` defaults evaluate in the session time zone on a `DATETIME` column, so two clients would have written different values for the same instant -- the precise failure the spec's own user story exists to prevent, in DDL no application code could override. And the include/exclude judgement sat on the page row, which left its stickiness to whatever the not-yet-written upsert happened to do; it now lives against the address, where nothing can forget it.

Three findings were breaches of rules this repository sets for itself: a direct dependency marked indirect, a deletion command the conventions forbid, and the crawl target's domain and page counts written into prose that the naming rule reserves for the configuration file.

**Alternatives rejected:** Deferring the judgement calls. The vector ceiling appeared as a bare number in three places with nothing linking them, and a column named only "context" meant nothing to a reader; both are cheap now and expensive once code depends on them.

**Recorded by:** code-review

## 2026-09-07 21:13 PDT - Telemetry columns are specified rather than removed

**Decision:** Keep the model, latency, trace and conversation-turn columns on the query log, and add user stories for them.

**Why:** The review correctly found them unrequested. They are features of the original system, and the governing rule of this port is to mirror its features, so the fault was in the spec omitting them rather than in the schema carrying them. The column that held recent conversation turns was also renamed, because its old name named nothing.

**Alternatives rejected:** Removing them until a later spec asks. That would mean adding columns to a live table for a feature already known to be coming.

**Recorded by:** code-review

## 2026-09-07 21:27 PDT - Second review: the live test tier is written

**Decision:** Act on all thirteen findings of the second two-axis review. The largest is that the live tier now exists: ten tests asserting the rules the database enforces, run against a real server.

**Why:** The first round of fixes introduced three faults of its own. Two comments asserted that a live-tier test checked the vector width when no live test existed at all. Worse, the check script's live step ran a name pattern that matched nothing and reported success, which is precisely the failure its own comment claimed to prevent. Writing the tests makes both claims true rather than deleting the claims.

The guard on that step needed a second attempt as well. Failing when the output contained the phrase for an empty run was wrong: a package with no live tests prints it legitimately while another package runs its whole suite. The condition is whether any package ran anything. Verified in both directions, healthy and vacuous.

**Alternatives rejected:** Deleting the comments that cited a test which did not exist. That would have left the vector width, the timestamp behaviour and the uniqueness rules asserted by nothing.

**Recorded by:** code-review

## 2026-09-07 21:27 PDT - The permitted vector width is a single value

**Decision:** The configuration validator requires the vector width to equal the application constant exactly, rather than merely not exceeding it.

**Why:** A narrower vector is as unusable as a wider one. The column is declared at one width, and the embedding function exposes no options argument, so it always returns exactly that many elements. A configuration naming any other number describes a system that cannot store what it produces.

**Alternatives rejected:** Accepting anything up to the maximum, which is what the specification literally said and what the previous code did. It left the gap unguarded.

**Recorded by:** code-review

## 2026-09-07 21:38 PDT - Third review: the guard was broken and my verification of it was worthless

**Decision:** Fix all fourteen findings. Two matter more than the rest, and both were faults in the previous round's fixes.

**Why:** The check script's live-tier guard did not work. It counted lines matching a pattern that excluded one shape of Go's output and not the other, so a live tier that had been deleted or renamed still reported green. Both review axes found it independently and both verified it by running it.

The failure behind that failure is the one worth recording. I had tested the guard by copying the script to the temporary directory and running it there. The script begins by changing to its own directory, so it ran outside the module, the test command failed for an unrelated reason, and the script took a different failure branch entirely. I read the word FAIL and reported the guard verified. It was never exercised.

Two changes follow from that, not one. The guard now counts passing tests from the machine-readable output of the test runner, so there is no pattern to get subtly wrong. And a fix to a test harness is now itself tested in place, from inside the module, in both directions: a healthy run must pass and a deliberately empty one must fail.

**Alternatives rejected:** Correcting the pattern. It would have worked, and it would have left the next reader trusting a regular expression over human-readable output that the tool is free to reword.

**Recorded by:** code-review

## 2026-09-07 21:38 PDT - The fast tier is closed to the database by construction

**Decision:** The live tier skips under the short flag as well as when no connection string is present.

**Why:** It previously gated only on the connection string, so on any machine with a server configured the fast tier ran the entire live tier. That silently destroyed the property the two tiers exist to provide, and it did so most on exactly the machines where the tests are run most.

**Alternatives rejected:** Relying on the check script to pass the flag correctly. The property belongs in the tests, where it holds however they are invoked.

**Recorded by:** code-review

## 2026-09-07 21:38 PDT - New tests are mutated before they are trusted

**Decision:** Each of the three tests added this round was verified by breaking the thing it tests and confirming it fails.

**Why:** A test that has only ever passed has not been shown to test anything. Committing the schema's foreign key, the rollback and the upsert isolation to assertions that had never failed would repeat the mistake this round exists to correct.

Worth recording: the first mutation attempt weakened the assertion rather than the behaviour, so the test passed and proved nothing. A mutation must change the system under test, never the test.

**Recorded by:** code-review
