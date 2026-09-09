# Decisions

Every decision the user made, newest last. The reasoning behind each one is here; `PLAN.md` holds the resulting shape.

The opening entries were written in one sitting at the close of the planning session, because the repository did not exist while those decisions were being made.

A timestamp is the moment an entry was written, not the moment the decision was taken. Several later entries share a stamp to the minute, which means they were recorded together at the end of a working session rather than one at a time. That is a departure from the instruction to append each entry the moment a decision lands, and it is recorded here rather than hidden by inventing separate times. The cost of the departure is that the order within a shared stamp carries no information about which decision came first.

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

**Alternatives rejected:** One row per address, which would mean a page under review stops answering. Rejected because it is a regression that was already found and fixed once in Erdac, where a refresh took roughly a fifth of the live site offline. Dropping review entirely was also rejected, as it is the product's main safety property.

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

## 2026-09-07 22:24 PDT - Publish discards candidates on excluded addresses

**Decision:** A publish deletes any candidate whose address is excluded, rather than leaving it pending, and reports how many it discarded.

**Reversed the same day.** See "Publish leaves candidates on excluded addresses alone" below. This entry is left as written because it records what was decided at the time.

**Why:** An excluded address is never fetched again and never searched. A candidate sitting on one is a review item that can never be actioned and never goes away, so it accumulates in the review screen as permanent noise. The specification says publish promotes every included candidate; it does not say what becomes of the rest, and leaving them is the only other option.

**Alternatives rejected:** Leaving them pending, which grows a list of items an administrator can neither approve nor clear. Deleting them at the moment of exclusion instead, which would make excluding an address destroy a page that has not been reviewed yet, so a mistaken exclusion could not be undone by re-including.

**Recorded by:** none

## 2026-09-07 22:24 PDT - The fetch lock expiry is a constant, and it is a guess

**Decision:** A fetch lock older than thirty minutes is treated as abandoned. The value is a package constant, not configuration.

**Why:** Two things need the same number, and they must not be able to disagree: taking the lock, and refusing a publish while one is held. A constant is the only shape where that is true by construction.

Thirty minutes is a judgement and is marked as one in the code. No full crawl has been timed, so nothing measured supports it. The failure it must avoid is expiring a live lock, which lets two crawls interleave into one candidate set, so the value has to stay comfortably above the longest a healthy fetch can take.

**Alternatives rejected:** A configuration key, which lets the two callers be given different values and asks an administrator to choose a number nobody has measured yet. A parameter on each method, which pushes the same problem onto every call site.

**Recorded by:** none

## 2026-09-07 22:24 PDT - Opening the database forces four connection settings

**Decision:** `store.Open` rewrites the connection string to set time parsing, a UTC location, the session time zone and multiple statements per call, rather than requiring the caller to supply them.

**Why:** Every one of the four is silent when it is wrong. Without time parsing a timestamp arrives as bytes; without a UTC location it is parsed in the machine's zone; without the session zone pinned the server's own time functions evaluate somewhere else again; and without multiple statements per call a migration file cannot be applied at all. Three of those four produce a system that runs and records the wrong instant, which is the failure the whole timestamp design exists to prevent.

**Alternatives rejected:** Validating the string and refusing a wrong one, which is correct and pushes four pieces of dialect knowledge into every place a connection string is written.

**Recorded by:** none

## 2026-09-07 22:24 PDT - The migrator asks the server for its vector width

**Decision:** Before applying anything, the migrator calls the vector extension's maximum-dimension function. A server that does not know the function does not have the extension, and the migration is refused.

**Why:** The schema declares a vector column. Applying it to a server without the extension fails part-way through, on a type the server does not recognise, leaving the earlier tables created and the error naming a type rather than a missing extension. Since migrations cannot roll back, that half-applied state is what a person then has to diagnose.

The same call answers the second question worth asking, which is whether an embedding would fit at all.

**Alternatives rejected:** Reading a catalogue table for installed extensions, which is a name this port would have to guess at and which says the extension is present without saying it works.

**Recorded by:** none

## 2026-09-07 22:34 PDT - Publish leaves candidates on excluded addresses alone

**Decision:** Reverse the decision above. A publish promotes the included candidates and touches nothing else. A candidate on an excluded address stays where it is.

**Why:** The specification says publish promotes every included candidate; it does not ask for the rest to be destroyed, and its Out of Scope section refuses the neighbouring case in the same terms: addresses that vanish from the sitemap are reported, never removed. The review that found this was right that deleting is the widening.

The consequence decided it. A candidate is a page nobody has looked at yet. Deleting it on exclusion means an administrator who excludes an address by mistake cannot get that page back by re-including it, and nothing warned them. Leaving it costs one stale row per excluded address, which is bounded because an excluded address is never fetched again, and the review screen already knows which addresses are excluded.

**Alternatives rejected:** Keeping the deletion and documenting it, which was the previous decision. It trades a recoverable, bounded annoyance for an unrecoverable loss.

**Recorded by:** code-review

## 2026-09-07 22:34 PDT - The fetch lock is measured entirely on the server's clock

**Decision:** The lock's start time is written by the server with its own clock, and its age is read against that same clock inside the statement that locks the row. Taking the lock no longer accepts a time from the caller, and the publish timestamp is no longer used to judge whether a fetch is running.

**Why:** The first version passed the caller's time in for both. Publish then judged a running fetch against the timestamp it was asked to record the publish under, so a caller stamping a publish far enough ahead made a live lock read as abandoned and promoted a half-written candidate set. That is exactly the failure the lock exists to prevent, and the live test written alongside it passed by exercising the bug: it moved the clock forward to expire a lock, which is a thing no real caller can do.

Writing the start time on one clock and judging it on another is the same fault more quietly. A machine whose clock runs behind sees its own fresh lock as already abandoned. One clock is the only arrangement where an age means anything, and the server's is the one every client shares.

This does not contradict the rule that the application writes every time in UTC. That rule exists because a column defaulting to the current time evaluates in the session's time zone. The server's UTC clock function names its zone, and the schema file already uses it to record when a migration ran.

**Alternatives rejected:** Keeping the caller's time and documenting that it must be the real one, which is a rule the type system cannot state and the next caller cannot see.

**Recorded by:** code-review

## 2026-09-07 22:34 PDT - Storing a candidate looks the row up before writing it

**Decision:** Replace the insert-with-on-duplicate-clause with a lookup and then an insert or an update, inside one transaction.

**Why:** This dialect has no way to return the identifier of a row it just wrote, so the on-duplicate form recovered it by assigning the identifier to itself inside the update clause. MySQL skips the whole update when every assigned value already equals what is stored, and the assignment is skipped with it. Re-fetching an unchanged page would then report no identifier at all, and the chunk write that follows would fail against a page that exists.

The exposure was narrow, because the fetch time normally differs on every fetch. Fix it anyway rather than relying on that, because the failure it produces -- chunks refused for a page that is right there -- says nothing about its own cause.

The lookup costs one more round trip per page and cannot behave that way. It also closes a gap the previous version had: the row it finds is locked until the transaction ends, so two fetches of one address cannot both decide to insert.

**Alternatives rejected:** Reading the identifier back with a second query only when the first reported none, which is the same extra round trip on the path that matters and leaves the surprising statement in place for the next reader to trip over.

**Recorded by:** code-review

## 2026-09-07 22:34 PDT - Two-axis review of the store: every finding acted on

**Decision:** Act on all findings of the standards and specification review of the store, the migrator and their tests. Three were defects, three were documents breaking this repository's own rules, and the rest were judgement calls taken as improvements.

**Why:** The three defects are recorded above in their own entries: the fetch lock judged on the wrong clock, the identifier that could come back empty, and a publish that deleted unreviewed pages. Two smaller ones were fixed without their own entry, because neither had an alternative worth recording: the translation that reported any column too wide as an address too wide, so an overlong title sent a reader to inspect the wrong field; and the embedding write, which was a loop of separate statements and could leave a page embedded in part.

The document faults were breaches of rules this repository sets for itself. Two deferred-work entries were written for a reader who had already read the other files, which the audience rules forbid. A porting entry used a confidence word the agent instructions do not list. The instructions and the glossary disagree on that word, and the instructions were followed.

The judgement calls: a state type and a page identifier that nothing read were deleted, because they were a second copy of vocabulary the queries already spell out and could drift from it; three near-identical blocks in publish became one helper, because each was a separate chance to drop the row count it returns; two helpers that ran in opposite directions and differed only by a suffix were renamed; and test fixtures naming plausible page paths were replaced with neutral ones, which the naming rule requires.

Worth recording: the live test written for the fetch lock passed by exercising the bug rather than the behaviour. It expired a lock by moving the clock forward, which is something the caller controls only because the code was wrong. Before trusting a new test, check that it still passes for a reason the fixed code provides, not one the broken code provided.

**Alternatives rejected:** Deferring the deletion of the unused state type, on the grounds that a later specification will want it. A later specification can add it back knowing what it is for.

**Recorded by:** code-review

## 2026-09-08 15:43 PDT - The two entry files keep different words for an unconfirmed entry

**Decision:** `PORTING.md` keeps _unmeasured_ and `CONTRIBUTIONS.md` keeps _researched_. Neither file changes. The agent instructions and the glossary are corrected to describe what the two files actually do, because both described it wrongly.

**Why:** The conflict was reported as the two governing documents disagreeing with each other. Reading the entries themselves showed something else: each file is already consistent, and each already carries its own header line defining its own word. `PORTING.md` uses _unmeasured_ in four headings and _researched_ in none. `CONTRIBUTIONS.md` uses _researched_ in five and _unmeasured_ in none. The practice was never in doubt; only the two descriptions of it were.

The words should stay different, because they are not the same claim. _Unmeasured_ is a negation: nobody has watched this happen on a running server. It is always safe to say, and everything unconfirmed qualifies. _Researched_ is a positive claim that someone read another project's documentation and source, and `CONTRIBUTIONS.md` uses it as the gate on filing a report at that project's issue tracker. "I did not measure it" is too low a bar to clear before telling a stranger their software is broken.

**Alternatives rejected:** One word across both files, which reads tidier and lowers the filing bar to a negation. Relabelling the four `PORTING.md` entries to _researched_, which would also have overclaimed: an entry that rests on inference rather than on reading has no honest label under that scheme, so it would be called researched and would be lying.

**Recorded by:** none

## 2026-09-08 15:57 PDT - The site survey was run, and it corrected the documents that described it

**Decision:** Run the survey read-only against the live site, write the checklist and its findings into `PLAN.md` as a new section, and point the agent instructions at that section. Chosen from three options presented for review; the two alternatives were moving the hazard list between files, and correcting the broken pointer without surveying anything.

**Why:** `CLAUDE.md` told a reader that `PLAN.md` held the full hazard list and a site survey checklist. `PLAN.md` held neither. It named four of the nine hazards in prose, called them two, and never used the word checklist. The fuller list was in `CLAUDE.md` itself, directly above the sentence pointing away from it.

Correcting the sentence alone would have left the next specification, which covers fetching pages, with nothing to be written against. Every defence against an ingest hazard is a number in `siteconfig.toml`, so measure the site before choosing any of those numbers.

**Alternatives rejected:** Moving the nine hazards into `PLAN.md` and writing a checklist beside them, which makes the pointer true and still leaves the checklist written from belief rather than from the site. Correcting the pointer only, which is one edit and defers the real problem.

**Recorded by:** none (options presented via lavish)

## 2026-09-08 15:57 PDT - What the survey found, and what it disproved

**Decision:** Record all eleven checks and their answers in `PLAN.md`, correct the one hazard the survey disproved, and add the two it found. Leave `siteconfig.toml` untouched: the survey reports, and changing what the crawler does belongs to the specification that covers fetching.

**Why:** The survey disproved a rule this repository had been carrying as fact. Alias stubs were described as capitalised slugs that redirect, to be handled by following redirects and normalising case. Measured, they do none of that. They return success with a refresh instruction in the head, so following redirects removes none of them. About half carry no capital letter, so a capitalisation rule misses most of them. What does separate them is their shape and an instruction not to index. Every stub's real target is listed separately in the sitemap already, so dropping every stub loses nothing.

Two hazards nothing on the list covered. A sitemap entry can be a bare relative string rather than an address, and one containing a colon parses without error into a scheme name, so resolving it returns the string unchanged instead of an address; the failure is silent and produces no page. Separately, extracting body text with a pattern rather than a parser leaks attribute contents, including script source, into the text that gets embedded. The second was observed while running the survey itself, using a pattern-based stripper.

Three findings need a decision before the fetching specification is written, and all three are reported rather than acted on: the stub filter cannot be a capitalisation rule, the sitemap needs validating rather than trusting, and the configured contact address points at a page the body-length floor will drop, which leaves a configured topic with no source.

**Alternatives rejected:** Editing `siteconfig.toml` in the same pass. The survey's job is evidence; changing the crawler's behaviour on that evidence is the next specification's, and mixing the two would land untested values in the file that governs the crawl.

**Recorded by:** none

## 2026-09-08 16:12 PDT - The search path runs on one pinned connection

**Decision:** The session variables that carry the query vector and the API key, and the statement that reads them, run on one connection taken with `Connx`, the driver call that hands back a single dedicated connection rather than any free one, and released with a deferred close. Not on the pool, and not in a transaction. Recorded as an architecture constraint before the search path is written, and asserted by a live test.

**Why:** A session variable belongs to the connection it was set on. Go's `database/sql` is a pool that hands out an arbitrary free connection per call, so the statement that sets the variable and the statement that reads it can land on different connections, and the read returns NULL. The prescribed search path is two statements and two variables, so both are exposed.

The failure is worth naming precisely. It is intermittent: it only fires once more than one connection is free, so it passes in development and fails under load. And the symptom is unmeasured. A NULL query vector most likely produces a NULL similarity for every row, which fails the cutoff and declines every question, but it could also fail on scan. The cause is certain; the symptom is not, and this entry does not claim otherwise.

A connection rather than a transaction, because search performs no write and must stay runnable under a read-only account. A transaction is a wider instrument than the problem needs.

Worth recording: this repository already knew the hazard twice and neither place was connected to the search path. The schema tests explain it for `USE` and pin the pool to one connection. `store.Open` sets the session time zone as a connection-string parameter rather than with `SET` for exactly this reason. When a comment explains a hazard, search for every other place the same hazard applies before considering it handled.

**Alternatives rejected:** A transaction around the pair, which works and gives up the read-only property for nothing. Pinning the whole pool to one connection, which serialises every request in the server.

**Recorded by:** none

## 2026-09-08 16:12 PDT - The question that would remove the pin is asked by a test

**Decision:** Add a live test asserting that a bound parameter through the vector constructor is still rejected, and write its failure message as an instruction to update the documents.

**Why:** Whether the session variable is needed at all comes down to one unmeasured question: does the vector constructor accept a bound parameter? A bound parameter is rejected as the distance function's own argument, measured, and the recorded reason is that the type is inferred before the parameter is known. That reason predicts the constructor rejects it too, but nobody has asked.

If the prediction is wrong, the query vector needs no session variable, search becomes one pooled statement, and the constraint recorded above is unnecessary for the vector. That is a better outcome than confirming it, so the test is written to fail loudly on the good news rather than to pass quietly on the expectation.

Pinning a limitation this way costs one red run on the day it lifts, which is exactly the day someone should be reading the documents that describe it.

**Alternatives rejected:** Logging the answer instead of asserting it. The check script does not run tests verbosely, so a logged finding would never be read.

**Recorded by:** none

## 2026-09-08 16:19 PDT - Five-agent review: acted on, with two findings declined

**Decision:** Act on the critical findings and the error-handling findings of the five-agent review. Two style findings are declined; the entry below this one gives them and the reasons.

**Why:** Seven of the eight critical findings were real and are fixed. Each is one sentence here because seven reasons in one paragraph is a paragraph nobody finishes.

The migration package moved under `internal`. At the module root it published a function that executes schema statements against any handle it is given.

The check that the vector extension is present moved into the store and now recognises only the server's unknown-function refusal. Reporting a dropped connection as a missing extension sends an operator to install something already installed.

The connection pool gained limits. The model functions hold a server thread for a whole outbound call, so an unbounded pool exhausts the server rather than making callers wait.

A deadlock and a lock-wait timeout became their own sentinel. They are the two failures where retrying is correct and every other error means the opposite.

Publish now states its isolation level, which is the setting that decides how much one transaction is shielded from what others are doing at the same time. Its comment claiming the candidate set is locked is only true under a setting where locking a set of rows also blocks an insert that would land among them. The weaker setting many servers run locks the rows it found and lets a new one appear beside them.

Three sentinels that no test touched are now asserted, including the one guarding the single constraint the design rests on.

The fetch lock's documentation now says plainly that it is advisory and that every candidate writer must take it. The previous wording read as though the database enforced it.

The eighth was the pooled session variable, already fixed earlier the same day.

**Alternatives rejected:** Adding an automatic retry around the transient sentinel, which the review proposed. Retrying is correct and the sentinel now makes it possible, but how many times and how long to wait is a policy the caller owns, and a store that silently retries turns a fast failure into a slow one without asking. The sentinel and its documentation ship; the retry belongs to the code that calls it.

**Recorded by:** all-review

## 2026-09-08 16:19 PDT - Two review findings declined

**Decision:** Do not apply the rule that every function must end in `return nil`. Do not split `Publish` into named steps.

**Why:** The first is not a rule this repository has. It comes from the reviewing agent's own guide, and that agent could not run its own analysis script, so it applied the guide by hand. `return rows.Err()` and `return tx.Commit()` are ordinary Go. What the finding was actually right about is narrower and is fixed: several of those returns handed back an unwrapped error while every sibling branch in the same function wrapped one, so a log filter matching the operation name found some failures and missed others. Every failure path in a function now reports the same way. The blanket rule would have changed nine sites to fix five.

The second is a case where the reason given argues against the change. `Publish` is long because it is one transaction whose step order is the specification: retire, then promote, then delete. Promoting first collides with the live page and the database refuses it, which is a bug the original system shipped. The review calls the steps unrelated context; they are the opposite, and turning them into a sequence of calls makes the order easier to change without noticing. Keep the three steps in one body so a reader reordering them has to read the comment that says why the order is load-bearing.

**Alternatives rejected:** Applying both and recording the disagreement afterwards. When a finding's own stated reason argues against its fix, decline it and record why, rather than applying it and noting the doubt underneath.

**Recorded by:** all-review

## 2026-09-08 16:38 PDT - A survey record may state structural shape

**Decision:** Add one exemption to the naming rule. A survey record may state the structural shape of the crawl target where a defence is built from that shape: how many segments an address has, what kind of page a group of addresses holds, what chrome every page carries. Counts, addresses, the company name, the brand and the domain stay barred.

**Why:** A standards review found that `PLAN.md`'s survey states page structure, which the naming rule reserves for the configuration file, and that the section's own claim to give "shapes rather than addresses" does not clear it, because shape is what the rule means by structure. The review was right and the wording was a way of not noticing.

The rule as written leaves a survey unable to record its answers. Its most useful result is that a stub is identified by having one path segment while every real page has more, and that sentence is exactly what the rule forbids. Without it the next specification re-derives the discriminator by hand, against a live site, having been told a survey was already run.

The exemption is bounded to what a defence is built from, so it licenses the discriminator and not a description of the site. `CLAUDE.md` already contemplated site observations entering a document after generalisation; this states where the boundary falls instead of leaving it to be guessed each time.

**Alternatives rejected:** Moving the shape facts into the configuration file, which is licensed to hold them. It splits one survey record across two files and puts prose in a file of values. Stripping the shape facts and keeping only the verdicts, which is the strictest reading and discards the single most actionable thing the survey produced.

**Recorded by:** code-review

## 2026-09-08 16:38 PDT - Standards review: fourteen findings fixed, one raised as a question

**Decision:** Act on every standards finding except the naming question, which was put to the user and is recorded above.

**Why:** Two were hard violations of a rule with a real consequence. The schema test tier applied its migration by a hardcoded path, so the day a second migration lands that whole tier would pass against a schema it is no longer testing; it now runs the embedded set. And the driver-error translation was applied to some statements and not others in the same function, so a deadlock met on one statement reached a caller as a retryable conflict and on the next as an opaque failure.

The rest were consistency and idiom: a three-value lock reading that always travelled together became one value with a method on it, the rune count stopped allocating, the sort and the file read moved to current library calls, and the lint configuration gained the pinned-connection close that an architecture constraint mandates and the configuration would otherwise have flagged on every use.

Three findings were about the documents rather than the code. Aphorisms were replaced with the instruction each implied. A glossary term that had acquired two meanings in adjacent entries was disambiguated. And the decision log's own header claimed every entry after the backfill was appended as it happened, which several shared timestamps disprove; it now states that a timestamp is when an entry was written and that recording in batches is a departure from the instruction rather than a convention.

**Alternatives rejected:** Inventing distinct timestamps for the batched entries, which would have satisfied the letter of the rule by fabricating the evidence it exists to preserve.

**Recorded by:** code-review

## 2026-09-08 16:50 PDT - Live tests live beside the code they test

**Decision:** Fold the schema live tests into the migrations package and delete the package they were in. A live test belongs in the package holding the thing it exercises, so there is no separate question of where one goes.

**Why:** The schema SQL moved into the migrations package earlier the same day, which left the package it came from holding a single test file and named for something it no longer contained. Two live-tier homes then existed with nothing in either name saying which one a new test belonged in, so the split would have drifted by whatever each author guessed.

**Alternatives rejected:** Renaming the package to what it tested. It keeps two homes and needs a rule written down to tell them apart, where folding removes the question.

**Recorded by:** code-review

## 2026-09-08 16:50 PDT - The live-tier guard walks the module rather than reading a list

**Decision:** The test that checks every live test is named so the check script selects it now walks from the module root for live-tier files, instead of iterating a list of paths.

**Why:** The list was the same failure one level up. A live-tier file added anywhere the list did not name went unguarded, and the guard reported success having checked nothing, which is precisely the outcome its own comment says it exists to prevent. Folding the schema tests into another package would have moved a listed path on the same day.

It also now fails when it finds no live-tier file at all, because a guard that checks nothing must say so rather than pass. Both directions were verified by breaking them: a test renamed in the file the old list did not name is caught, and hiding every live-tier file fails the guard.

**Alternatives rejected:** Moving the check into the check script, beside the pattern it protects. A shell test cannot be broken and re-run as easily, and this one earns its keep by being verifiable.

**Recorded by:** code-review

## 2026-09-08 16:50 PDT - The timestamp rule names its one exception

**Decision:** Amend the architecture constraint and the schema specification to say that a value whose purpose is comparison against the server's clock is written with the server's clock.

**Why:** Both documents said every time is written explicitly by the application in UTC, with no exception. The fetch lock's start time is written by the server, which the documents did not mention, so a reader following them would write it from the application and reintroduce the two-clock fault that was fixed earlier the same day.

This is not a loosening. The hazard the rule guards against is a function that evaluates in whatever zone the session happens to have; the server's UTC clock names its zone, and the migration file already uses it to record when a migration ran.

**Alternatives rejected:** Leaving the documents absolute on the grounds that the code is right. Amend a rule as soon as correct code has to disobey it, because the next person writes what the rule says rather than what the code does.

**Recorded by:** code-review

## 2026-09-08 16:50 PDT - The store's entry file splits by what changes it

**Decision:** Split the store's largest file into three: the handle and its connection settings, the failures the package tells apart, and the values that cross its boundary.

**Why:** It had grown to hold pool constants, every sentinel, the constructor and its lifecycle methods, six domain structs and four helpers, so it changed for several unrelated reasons and every one of those changes was reviewed against the rest. The package already had the shape to follow: the files holding pages, publishing and system state are each named for one concern.

Two details worth recording. The lock's expiry constant moved to the file holding the lock it tunes, rather than staying with the handle. And only one file keeps a comment touching the package clause, because a comment in that position is the package's documentation and three of them would leave which one appears to chance.

**Alternatives rejected:** Leaving it, on the grounds that the file was internally consistent. It was, and that is not the property being asked for.

**Recorded by:** code-review

## 2026-09-08 16:54 PDT - The vector literal drops exponent notation

**Decision:** Format every element of a stored vector as a plain decimal, never in exponent form, and give the live tier a vector shaped like a real embedding instead of one filled with a single convenient value.

**Why:** The shortest representation of a float switches to exponent notation below roughly one ten-thousandth. A normalised embedding is full of elements that small and of negative ones, so the literal the ingest path writes would have contained exponents on real data. Whether the column's parser accepts one has never been asked of the server, and it does not need to be: a decimal expansion is accepted by any reading of the format.

The reason this was not caught is the part worth recording. The only live coverage of the vector write filled all three thousand elements with one positive value just above a tenth. It proved the round trip for numbers a real embedding almost never contains, so a formatting choice that would fail on real data passed it. Shape a live fixture like the data the path will really carry: for a vector, that means negative elements and magnitudes small enough to exercise how a number is written down.

**Alternatives rejected:** Keeping the shortest form and asking the server whether it parses an exponent. The question is interesting and the answer would go in the porting notes, but the system does not need it answered, and shipping a dependency on an unmeasured behaviour to find out is the wrong order.

**Recorded by:** code-review

## 2026-09-08 16:54 PDT - An abandoned fetch refuses a publish

**Decision:** A fetch lock that is present but expired refuses a publish, and still permits a new fetch to take it over.

**Why:** Two stories in the specification were satisfied separately and left a hole between them. One expires a stale lock so a crashed run does not lock the system out. Another refuses a publish while a fetch is running. After a crash mid-crawl the lock expires, so a publish saw no lock and promoted whatever the dead run had written, which is half of one crawl in front of visitors, with nothing reporting it.

Expiring a lock and trusting the pages written under it are two decisions, and running them together made the second one invisible. Only the first was ever asked for.

Worth recording: a test asserted the wrong behaviour here, and the code obeyed it. It was named for a crashed run not blocking publishing forever, which sounds like the story about not being locked out and is not the same claim. Read a new test's name against the story it says it covers, word for word, because a name that merely sounds like the story can pin the opposite of it.

The recovery is a fetch that runs to completion, which replaces the suspect candidates and releases the lock. An administrator who does not want to re-fetch has no other route today; that affordance is recorded in `DEFERRED.md` against the administration pages.

**Alternatives rejected:** Recording the interaction and leaving the behaviour, which was the reviewer's other option. It leaves a silent corpus corruption behind a note. Refusing a new fetch as well, which would restore the lockout the expiry exists to prevent.

**Recorded by:** code-review

## 2026-09-08 16:54 PDT - What was built beyond the specification is written into it

**Decision:** Add two sections to the schema specification: what implementation added that the specification did not ask for, and what the specification asked for that was not built.

**Why:** Four sentinel errors, an extension preflight, pool limits and the abandoned-fetch refusal were all added during implementation, each for a reason recorded in this log, and none of them recorded against the specification. A reader treating that document as the description of the system would have been wrong about the system in six ways.

The second section matters as much. The query log has no writer, no consumer declares an interface over the store, and nothing compares a sitemap to stored addresses, all because the work those belong to is specified separately. Saying so in the specification is what keeps a later reader from assuming the stories covering them were met.

**Alternatives rejected:** Recording the additions only in this log. It is the right place for why each was added and the wrong place for what the system currently is.

**Recorded by:** code-review

## 2026-09-08 17:05 PDT - The specification's own count of the sentinels is checked by a test

**Decision:** Enumerate every sentinel error in the specification with the reason for each, state how many there are, and add a test that fails when the number stated there and the number declared in code disagree.

**Why:** The section written yesterday to record what implementation added said four sentinels beyond the ones specified. There were eight. Three of the missing ones carry behaviour no story states, and one of those refuses an operation: a run whose lock expired is refused its release, because clearing it would release a lock a live run is relying on.

The section was added so the specification would stop describing an earlier version of the system, and it described an earlier version of itself within a day. Check a stated count against the thing it counts, in a test, whenever the thing it counts is a list that keeps growing.

The test is the point rather than the enumeration. Listing them is what a reader needs; counting them is what keeps the list honest, and a count nothing verifies drifts exactly the way this one did.

**Alternatives rejected:** Removing the count and enumerating only, which fixes the wrong number by deleting it and lets the next sentinel be added with no entry at all.

**Recorded by:** code-review

## 2026-09-08 17:05 PDT - A redundant index is dropped, and the class is guarded

**Decision:** Remove the separate index on a chunk's page. Add a live test refusing any non-unique index whose columns are the opening columns of another index, in the same order, which is the shape that makes one index unusable while the second exists.

**Why:** The uniqueness rule on a page and an ordinal already indexes the page as its first column, so a lookup by page uses that rule's index and the second index on that column alone could never be chosen instead, while the server maintained it on every insert and delete. The foreign key is satisfied by the uniqueness rule for the same reason.

Every other index in the schema was checked against the same rule and none is redundant. The two on the query log lead with different columns, and the one on an address is on a real column rather than on either generated one.

The test reads the catalogue rather than the schema file, so it also catches an index added to a database by hand and one added by a later migration. A unique index is exempt: its columns may be the opening columns of a longer one and it is still enforcing a rule no other index enforces.

Worth recording: the schema file was edited rather than corrected by a second migration, which is only allowable because no database holds this schema except the scratch ones the tests create and drop. That stops being true the moment one does.

**Alternatives rejected:** A second migration dropping the index, which is the correct shape once a database exists and is ceremony while none does.

**Recorded by:** code-review

## 2026-09-08 17:08 PDT - A resolved exception is deleted, not corrected

**Decision:** Remove the exception saying every question writes to the database before it can be answered. Add one for the fetch lock being advisory. Replace the page counts in three documents with a statement of shape.

**Why:** The exceptions file lists shortcuts currently taken, so an entry describing a shortcut no longer taken does not belong in it whatever it says. Search performs no write, measured on 2026-09-07, and that entry's own "what production requires instead" line asked for exactly what the system now does. Correcting its wording would have left a resolved problem sitting in a list of open ones. The reversal is recorded in this log and in the porting notes, which is where a reader looks for what changed.

Checking the rest of the file found something the note did not mention. Three documents carried the crawl target's page count, which the naming rule reserves for the configuration file, and a previous review had already caught that class of breach once. The number was also written before the site was surveyed, so it was both barred and wrong. All three now state the shape of the problem instead of its size.

A new exception was added while the file was open. The fetch lock is advisory: the database does not enforce it, and the guarantee that a publish never promotes a half-written set of pages holds because the code that writes pages takes the lock first. That is a shortcut with a production remedy, which is what this file is for, and it was documented in the code today without being recorded as one.

**Alternatives rejected:** Rewriting the stale entry to describe the current design. It is not an exception, so there is nothing for the entry to say.

**Recorded by:** code-review

## 2026-09-08 17:10 PDT - The two joined pages are read by one piece of code

**Decision:** Replace the eight parallel locals in the address listing with one nullable page type, scanned twice and converted by one method. Select a candidate's publish time as well, so both pages have the same shape.

**Why:** The query joins the same table twice, once for each state an address can hold. Reading the two results into separately named locals and building each by hand meant a column added to one and forgotten in the other would compile and report the wrong thing for exactly one of the two states, which is the harder half to notice.

The column order now sits next to the column list it has to match, in the method that returns the scan destinations, rather than being restated in a call twenty lines below it.

Selecting a publish time for the candidate costs one always-empty column and makes the two shapes identical. A page that has never been live saying so in the column meant for it is the honest form, and it removes the only asymmetry the two joins had.

Worth recording: the test covering this asserted only that both pages were present and that their content hashes differed. It would have passed with the two read from each other's columns, which is the mistake this query is most likely to make. It now asserts each page's values, and swapping the two scan orders fails it.

**Alternatives rejected:** Leaving it and adding a comment about keeping the two in step. Prefer a type that makes two things impossible to fill in separately over a comment asking the next author to remember to.

**Recorded by:** code-review

## 2026-09-08 17:13 PDT - The timestamp rule is restated, because naming one exception was wrong three ways

**Decision:** Split the rule in two. An absolute part: never a time function whose result depends on the session's zone. And a part with named cases: times the application has are written by the application, and three kinds of value take the server's clock because there is no application value to use.

**Why:** The rule was amended yesterday to name one exception, the fetch lock's start time. A review found a second use it did not cover, in the migration file. Counting the rest found a third: the live tests use the server's clock throughout for their fixtures. So the amendment was wrong three ways within a day of being written, and it was wrong in the direction that matters, because a rule stating one exception makes every other use look like a breach.

The mistake was fixing the sentence in front of me rather than restating the rule. The hazard has always been a function whose result depends on the session's zone, and a function that names UTC does not have it. The clock question is separate: values compared against each other must come from one clock, which is why the application supplies them where it has them. Written as two rules, every use in the repository is accounted for, and a fourth would be a bug rather than a fourth case.

The count was verified rather than asserted: every server-clock use in the repository was classified, and all of them fall into the three named kinds.

**Alternatives rejected:** Writing the migration's record from the application. The file is executed as raw statements with no parameters, so the application has no value to supply, and the file recording itself is what keeps a database brought up by hand from looking empty.

**Recorded by:** code-review

## 2026-09-08 17:13 PDT - Three doc comments broken by the file split are repaired

**Decision:** Reattach the doc comments that the split of the store's entry file left orphaned, and check every doc comment in both packages rather than only the three reported.

**Why:** The split was done by line ranges, which cut two comments in half. One function's opening sentence was left in the file its body no longer lives in, and another gained a stray blank line that ends a doc comment where it stands. The rendered documentation for three functions began mid-thought, and two files carried comments describing code they did not contain.

Neither the formatter nor the vetter reports this, and neither does the linter, so nothing but reading it catches it. The lesson is about the method: splitting a file by line numbers moves text without regard for what the text is attached to. After splitting a file, render its documentation and read it, rather than trusting the formatter to have noticed.

**Alternatives rejected:** Fixing the three that were reported. The same cut could have orphaned others, so every comment in both packages was checked for a blank line between it and what it documents.

**Recorded by:** code-review

## 2026-09-08 17:32 PDT - The fetch lock stays advisory for now, and the reason is written down

**Decision:** Leave the lock advisory. Do not add a renewal method. Record the whole gap, including the part no review has named, and settle it in the specification that covers fetching.

**Why:** Two reviews raised this as two findings and it is one hole with three parts, of which each review named one.

The lock expires after a fixed age, and nothing extends it, so a crawl that runs longer than that age has its lock taken by the next fetch. Nothing stops the first run writing after that, because the three methods that write pages do not check the lock at all. And the takeover erases the evidence: an expired lock sitting in the row makes a publish refuse, but once a second fetch overwrites it and finishes cleanly, the row shows a completed fetch and the publish promotes a set containing both runs' pages.

That third part is a defect in a fix made yesterday, which refuses a publish while an abandoned lock is present. It is bypassed by anything that takes the lock over.

None of the three fixes proposed closes it alone. Renewal narrows the window and a pause longer than the age reopens it. Checking the lock in the writers stops the loser writing any more and does not remove what it already wrote. Only discarding the previous run's pages does that, and doing so destroys pages an administrator may be part-way through reviewing.

So the missing piece is a decision, not code: what a takeover means for the pages the previous run left. That cannot be settled without the crawler's skip logic, because whether a completed run has replaced every page depends on what it chose to skip. The specification covering fetching is where it belongs.

**Alternatives rejected:** Adding a renewal method now. It has no caller, its correct interval is a property of a crawler that does not exist, and shipping it would make the hole look handled while leaving two thirds of it open. Making the writers check the lock now, which changes three signatures for a caller that does not exist yet and still leaves the corpus mixed.

**Recorded by:** all-review

## 2026-09-08 18:01 PDT - Aphorisms keep coming back, so the check moves to the end of the work

**Decision:** Replace the five conclusion-shaped sentences in this file with the instruction each was standing in for. Define the terms a reader meets here for the first time. Add a glossary entry for a term that had two meanings, one of them already defined for something else.

**Why:** A review found three of these. Looking for the rest found five, and all five were written after an earlier round of the same correction. Writing the aphorism and then removing it is not working as a method, because the sentence arrives sounding like the point and reads as finished.

What the rule is actually asking for is the sentence that tells a reader what to do differently. "A fixture chosen for convenience tests the fixture" is a verdict on work already done. "Shape a live fixture like the data the path will really carry" is the same observation aimed at the next person, and it is the half that was missing every time.

Three terms had no definition anywhere in this file: the driver call that pins one connection, the transaction setting that decides how much one transaction sees of another, and the index shape that makes one index unusable while a second exists. Each now carries a clause saying what it is, at the point it first appears.

The glossary gained an entry for a named failure that code returns instead of a message. The word was already defined there for something unrelated, the string a model replies with when it cannot answer, and this file uses it fourteen times in the other sense. Both entries now say the other exists.

**Alternatives rejected:** Deleting the sentences rather than completing them. Each was carrying a real observation; only the instruction was missing.

**Recorded by:** code-review

## 2026-09-08 18:05 PDT - One helper for the fetch-lock stub, and a mutation that proved nothing

**Decision:** Collapse the eleven hand-written stubs of the fetch-lock read into one helper and one named statement. Leave the two live-tier setups duplicated, which a deferred entry already covers.

**Why:** Eleven copies of a three-column stub is eleven chances to put the columns in the wrong order, and a wrong order does not fail loudly: it stubs a lock that reads as something else, so the test still passes and asserts the wrong situation. The column list now appears once.

Worth recording, because it is the second time this exact mistake has been made here. The first attempt to verify the helper swapped the column names and expected the tests to fail. They passed, and they were right to: the driver binds a row by position and the names are labels. The mutation changed nothing, so it proved nothing, exactly as an earlier round's attempt did when it weakened an assertion instead of the behaviour.

Swapping the values is the mutation that means something, and both orderings of it fail the lock tests. Before trusting a mutation, check that the thing changed is the thing the test is supposed to be watching.

**Alternatives rejected:** Separate helpers named for each case, such as one for no lock and one for a held lock. Two arguments that read as the row they produce keep the call sites honest about what is being stubbed, and the cases differ only in those two values.

**Recorded by:** code-review

## 2026-09-08 18:41 PDT - The first live run: three names too long, and a guard that counted the wrong test

**Decision:** Shorten three live-test names. Move the scratch-database name-length check into the fast tier. Anchor the check script's live pattern so that only tests beginning with the live prefix count as having run.

**Why:** The live tier ran for the first time and could not reach the server, because the connection string named the wrong user. That is a configuration matter. But three tests failed before reaching the server at all, on a check written into the live tier itself: each live test creates a scratch database named after itself, MySQL caps that name at 64 characters, and the check refuses to truncate. Three names were over the cap. The check was correct and it lived in the one tier that never runs, so it caught nothing for a day.

The same run exposed a second fault. The check script selects live tests with a pattern that matched anywhere in a name, and a fast-tier test named for guarding the live tier contains the word. It matched, it passed, and its pass alone satisfied the script's rule that at least one live test must have run. With every real live test deleted the script would still have reported the tier green, on the strength of a test that never touches a database. The pattern is now anchored to the start of the name.

**Alternatives rejected:** Truncating scratch names to fit, which two tests sharing one database would turn into results that depend on running order. Renaming the guard test to avoid the word, which fixes one collision and leaves the pattern open to the next.

**Recorded by:** none

## 2026-09-08 18:48 PDT - Stubs are skipped by shape, before fetching

**Decision:** The crawler does not fetch any sitemap address with a single path segment. Chosen from three options presented for review.

**Why:** The survey measured that every alias stub on this site has exactly one path segment and every real page has more, that the capitalisation rule in the configuration catches three stubs in eleven, and that every stub's real target is listed in the sitemap separately. Skipping by shape is therefore exact on this site and loses no page, and it costs nothing, where fetching every stub and dropping it afterwards is dozens of wasted downloads per crawl that only works because stubs happen to be empty.

The two addresses the configuration excludes by name are both single-segment, so this rule makes that list redundant. The specification covering fetching decides whether the list stays as documentation or goes.

**Alternatives rejected:** Fetching everything and relying on the body-length floor. Keeping the capitalisation rule, which is measured to miss most of them.

**Recorded by:** none (options presented via lavish)

## 2026-09-08 18:48 PDT - A malformed sitemap entry is skipped and named, not repaired and not fatal

**Decision:** When a sitemap entry is not an absolute address, the crawler skips it and the run's summary lists it by its exact text. The crawl continues. Chosen from three options.

**Why:** Two entries in this sitemap are bare strings containing a colon, which a URL parser accepts without error by reading the text before the colon as a scheme. Silently skipping them is what happens today, and nothing says so. Refusing the whole crawl for a typo in a file the site tool generates holds every other page hostage to it. Guessing a repair fetches a page that may not be the one intended and reports nothing about having guessed.

**Alternatives rejected:** Refusing the crawl. Resolving the entry against the site base.

**Recorded by:** none (options presented via lavish)

## 2026-09-08 18:48 PDT - The contact address points at the home page

**Decision:** `contact_url` names the home page with a fragment for its contact section. Chosen from four options, with the address supplied by the user.

**Why:** The previous value named a folder listing whose article element arrives empty, so the body-length floor dropped it and the topic about getting in touch had no source page. The home page's served text carries the contact details, confirmed against the copy fetched during the survey, and at well over the floor it is stored and searched like any other page. A fragment is never sent to the server, so the crawler fetches the home page and the fragment only steers a visitor who follows the link.

One thing is unverified: the served HTML did not contain an element with that fragment's identifier, so the link may land at the top of the page rather than at the section. That affects where a visitor lands, not whether the chatbot can answer.

**Alternatives rejected:** Writing the contact details into the facts list, which is a second place to keep current. Dropping the topic. Exempting the listing page from the floor, which would store a page of links.

**Recorded by:** none (options presented via lavish)

## 2026-09-08 18:48 PDT - After a crashed crawl, its pages are kept and publishing waits for a full crawl

**Decision:** When a new crawl takes over the marker a crashed one left, the crashed crawl's pages are kept. Publishing stays refused until a crawl runs in force mode, visiting every address rather than skipping ones that look unchanged, so that every waiting page is known to come from one run. Chosen from three options.

**Why:** This is the decision recorded earlier as the one that could not be made without the user, and it settles the three-part fix to the fetch marker. Keeping the pages destroys nothing an administrator was reviewing. Waiting for a force crawl is the only condition under which the waiting set is provably from one run, because a refresh skips pages and a skipped page keeps whatever version was there. On a site this size a force crawl is minutes.

What this unblocks, all in the specification covering fetching: a running crawl refreshes its marker so a slow crawl is not mistaken for a dead one; a crawl that has lost its marker is refused when it tries to save a page; a takeover is recorded so publishing knows to wait; and a completed force crawl clears that record.

**Alternatives rejected:** Deleting the crashed crawl's pages on takeover, which is simpler and destroys review work. Leaving the gap recorded, which is the outcome the review step exists to prevent.

**Recorded by:** none (options presented via lavish)

## 2026-09-08 18:48 PDT - The one page count in the log is replaced, and the rule was explained badly

**Decision:** Replace the page count in the earliest entry with a proportion. Nothing else changes, because nothing configures a page count.

**Why:** The user read the naming rule as requiring a page count to be configured in advance, and asked why that was needed when the crawl discovers the pages itself. It is not needed and nothing does it. The rule bans counts from every file except the configuration; it does not ask the configuration to hold one, and it does not. The explanation that produced the misreading said a count "may appear in one file only", which reads as permission to put one there rather than as a ban everywhere else.

The user's objection still lands on the entry it was aimed at. Its numbers described the original system's site, and they were the last page count anywhere in the repository. Replacing them with a proportion means the rule holds with no exception to remember, which is the outcome asked for.

**Alternatives rejected:** Leaving the entry as written under the log's convention. The convention protects reasoning from being rewritten; a number is not reasoning.

**Recorded by:** none (options presented via lavish)

## 2026-09-08 19:09 PDT - The live tier ran: thirty-two pass, and the one failure was the test

**Decision:** Compare a stored vector to what was written element by element at float32 precision, not by the spelling of its text form. Record what the first run against a real server measured, and promote the claims that rested on it.

**Why:** The live tier ran for the first time, with the credentials read from the installer's file rather than typed. Thirty-one of thirty-two passed. The one failure asserted that the string `0.000012` survived the round trip through the vector column; the server stored the value and read it back as `1.20000004e-05`, the same float32 number spelled the server's way. The round trip never promised a spelling. The test now parses each element and compares the numbers, and all thirty-two pass.

Three claims move from unmeasured to measured. A session variable set on one connection reads as NULL on another, which is the hazard behind the pinned-connection constraint. A bound parameter through the vector's string constructor is refused, which means the variable and the pin are both required rather than a caution. And the transaction that publishes gets the isolation level it asks for.

One question recorded as not needing an answer got one anyway. The column and the string constructor both accept exponent notation, measured with a probe against a scratch database. The plain-decimal formatting chosen yesterday was hedging against a limitation that does not exist. It stays, because it is proven and pinned and costs a few characters per element, but the code comment now says why it was chosen rather than claiming the question is open.

The run also corrected a number this log has repeated: the live tier is thirty-two tests, not thirty-five.

**Alternatives rejected:** Switching the vector literal back to the shortest form now that exponents are known safe. It would delete a passing test and a recorded decision to save a few bytes per element on a payload measured in tens of kilobytes.

**Recorded by:** none
