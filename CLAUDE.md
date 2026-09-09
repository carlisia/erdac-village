# Erdac Village

Grounded website chatbot with an admin portal. Retrieval-augmented answers over a knowledge base built from one website. Go, stdlib `net/http`, static frontend with no build step, one self-hosted VillageSQL instance running natively on this machine.

A port of [Erdac](https://github.com/carlisia/erdac), which is the same product in Python over Neon Postgres and pgvector. Read `PLAN.md` before changing anything structural.

## Audience rules

**This file is written for agents.** Terse, imperative, jargon assumed. Do not soften it.

**Every other document in this repository is written for humans, including non-technical ones.** When you create or edit any file other than this one, follow these rules:

- Keep every technical fact. Obscurity is the problem, not precision. Never simplify by deleting detail.
- Define each technical term on first use in that file. Assume the reader has not read the others.
- No aphorisms. A sentence that sounds like a conclusion and contains no instruction is not one. State the instruction.
- Name the referent. Never "the problem", "this pattern", "that risk" without naming it in the same sentence.
- One idea per sentence. If it needs two reads, split it.
- Stay short. Plain is not long. A revision that doubles the length has failed.
- Land three beats in order: what it is, the concrete consequence, what to do. The third gets dropped.

Applies to `PLAN.md`, `PORTING.md`, `CONTRIBUTIONS.md`, `CONTEXT.md`, `DECISIONS.md`, `DEFERRED.md`, `EXCEPTIONS.md`, `docs/**`, and every README.

## The two rules that govern this port

**Lean on the VillageSQL ecosystem wherever it reaches.** Embedding, similarity search and generation execute inside `mysqld` via `vsql_ai` and `vsql_vector`, even where doing that in Go would be faster, more reliable, or better instrumented. Rough edges are the deliverable.

**When the stack blocks a feature, pin it. Never route around it.** No fallback implementations, no Go reimplementation of something the database cannot do. The gap goes in `PORTING.md` with what was lost and in `CONTRIBUTIONS.md` with what would close it upstream.

One exception, and it is narrow. A workaround VillageSQL's own documentation prescribes is the ecosystem's answer, not a bypass, and gets written: the generated-column substitute for a partial unique index, `LAST_INSERT_ID()` for `RETURNING`, `ROW_NUMBER()` for `DISTINCT ON`, a child table for an array column. The test is whether their docs prescribe it. If you invented it to avoid the database, it is a bypass.

## Naming

System is **Erdac Village**. Single-tenant, configured per deployment.

Company name, brand, domain, page structure, page counts: `siteconfig.toml` only. Never in code, comments, docs, commit messages, prompts, or test names. Crawled content is exempt and keeps its source text verbatim. The rule holds even though the crawl target is the author's own site, because it is what makes this a template rather than a one-off.

Site-specific observations get generalized into hazards before they enter a document. Concrete cases stay as unnamed illustrations.

One exemption, and it is narrow. A survey record may state the **structural shape** of the crawl target where that shape is what a defence is built from: how many path segments an address has, what kind of page a group of addresses holds, what chrome every page carries. A checklist that cannot record its own answers is half a checklist, and the next specification would re-derive them by hand.

The exemption covers shape only. It permits no address, no company name, no brand, no domain, and no count. Proportions were always allowed and still are. A survey finding that can be written as a shape must be written that way, and one that can only be written as an address does not go in the document at all.

## PORTING.md

The record of where this system and Erdac diverge. Append the moment a divergence is found, never batch.

Every entry: what Erdac does, what this does instead, and **forced** or **chosen**. Forced means the stack left no option. Chosen means it did and this is the reason.

Mark an entry _unmeasured_ while it rests on research rather than on a running server. Promote it when observed. Correcting one is a better outcome than confirming it.

## CONTRIBUTIONS.md

Gaps in VillageSQL that would be worth fixing upstream. Aimed outward, at that project, not at this one.

Every entry names the repository, states the gap, and argues why it affects more people than this port. Mark an entry _researched_ until it is confirmed against a running server, then _measured_ with the date. The word for the unconfirmed state differs from `PORTING.md`'s _unmeasured_ deliberately: an entry aimed at another project asserts its author read that project's documentation and source, which is a stronger claim than not having measured. **Never file anything upstream while still marked researched.**

## DECISIONS.md

Append an entry the moment the user decides something. Never batch.

```markdown
## YYYY-MM-DD HH:MM PDT - Short title

**Decision:** Specific enough to act on.

**Why:** Reason plus the concrete consequence.

**Alternatives rejected:** What lost, and why.

**Recorded by:** <driving skill, or `none`>
```

`Recorded by` names the skill that **drove the session**, or `none`.

Test: did the skill shape what got decided, which questions were asked, and which alternatives surfaced? `grill-with-docs` drives. Write `none` for ordinary conversation and mean it; do not borrow a skill name for weight.

Presentation-only skills are annotated, not credited. `lavish` renders options for comparison; it decides nothing.

```
**Recorded by:** grill-with-docs (options presented via lavish)
**Recorded by:** none (options presented via lavish)
```

Timestamps: `TZ='America/Los_Angeles' date '+%Y-%m-%d %H:%M %Z'`

## Other tracking files

- `DEFERRED.md` - postponed work. Three parts per entry: what, why it matters, what unblocks it. All three or the entry is useless.
- `EXCEPTIONS.md` - demo shortcuts. Three parts: what we did, why acceptable here, what production requires instead.
- `CONTEXT.md` - glossary only. No decisions, no design. Add a term when two readers could reasonably disagree on it.
- `docs/adr/` - hard to reverse, surprising without context, real alternatives. All three, or it is not an ADR.
- `docs/specs/` - numbered specifications, one file each, written before the work. No external issue tracker.

## Conventions

- Go 1.25. Idiomatic Go, not a transliteration of Erdac's Python package layout. Mirror behaviour, not structure.
- Keep two seams: where pages come from, and where results go. A seam is a boundary, not a type. Each has one concrete implementation and no wide interface; a consumer declares the two to four methods it calls, beside the code that calls them, and one test double satisfies all of those declarations. This is what lets the fast test tier run without touching the world.
- Interfaces are defined at the consumer, not exported alongside their implementation.
- `go-toml/v2` matches keys **case-insensitively**, so `TOP_K` binds to `TopK` and `DisallowUnknownFields` does not report it. Strict decoding catches misspellings, not miscapitalisations. Pinned by a test.
- Never hard-wrap prose in markdown. One paragraph, one line. Wrap code comments at ~90 and commit messages at ~72.
- ASCII punctuation only. No em dash, en dash, or Unicode arrows anywhere. Use `-`, `-`, `->`. They are multi-byte and silently break grep, log scrapers, and substring assertions when retyped.
- User's shell is fish. Commands written for a human must run in fish; fish has no heredocs. Scripts take `#!/usr/bin/env bash`. Break anything over 60 chars with trailing `\`.
- Never `git add`, `git commit`, `git push`. Show `git status --short`. Never `git add -A` or `git add .`. Never `git reset` or `git restore --staged` unprompted.
- Never `rm`. Use `trash`.
- Secrets never enter the repository. `.env.local` is gitignored.

## Architecture constraints

- VillageSQL is **alpha**, self-declared, pre-1.0, GPL v2, no Windows support. Expect breaking changes.
- Server is the prebuilt dev-server tarball under `~/.villagesql`, run natively. Not a container: a `.veb` built on macOS is a Mach-O dylib and will not `dlopen` inside a Linux image.
- Extensions build against `villagesql-extension-sdk`, passed as `-DVillageSQL_SDK_DIR` or `-DVillageSQL_BUILD_DIR`. The extension READMEs claim a full server build tree is required. They are stale; the SDK is enough, and their own CI proves it.
- `vsql_vector` builds against the **dev/unstable ABI**, which requires an exact protocol match with the server, not a minimum. Pair the SDK and server versions. Any server bump may reject the extension.
- `vsql_mcp` has a path dependency on `../vsql-rust-sdk`. Clone that repo as a sibling directory or nothing builds. Install `cargo-vsql` from that clone, not from crates.io.
- Both extensions need `SET PERSIST vsql_allow_preview_extensions = ON` before `INSTALL EXTENSION`.
- `vsql_mcp`: leave `allow_write` OFF. Its README states there is no row ceiling on writes and an unqualified `DELETE` empties the table. Tool queries run under the `db_url` account's real GRANTs regardless of tool-level guardrails.
- `SVECTOR` caps at 3072 dimensions. Ours is 3072, at the ceiling with no headroom.
- No vector index exists. Extension-defined index types are unsupported by the server, so every similarity search is a sequential scan.
- **`SVECTOR::FROM_STRING(x)` is the function everything depends on.** It is undocumented, appearing once in the project's test suite. It accepts computed strings, user variables and function results, and returns a value `COSINE_DISTANCE` takes. Use it for every query vector.
- There is **no way to store an `ai_embedding()` result into an `SVECTOR` column from SQL.** `FROM_STRING` returns a dimensionless `SVECTOR`; a column requires `SVECTOR(N)`; nothing casts between them. Embeddings round-trip through Go and are written back as string literals, roughly 39 KB each.
- `ai_embedding` returns a binary-charset string. Wrap every call in `CONVERT(... USING utf8mb4)` or `SVECTOR` assignment fails with a misleading `Incorrect SVECTOR value`.
- Without `FROM_STRING`, `COSINE_DISTANCE` accepts its query vector only as a **true constant** or a **custom-type column**. A bound parameter, a user variable, and `CONCAT(...)` all fail with `argument 2 must be a custom type or string constant`. A function result is not a constant, so `COSINE_DISTANCE(col, ai_embedding(...))` cannot work.
- A question's embedding therefore goes into a session variable, through `SVECTOR::FROM_STRING`, and inline to `COSINE_DISTANCE`. **Search performs no write** and can run under a read-only account. Storing it on a row was the design through two earlier revisions and is no longer needed.
- **A session variable belongs to one connection, and `database/sql` is a pool.** It hands out an arbitrary free connection per call, so `SET @v` and the statement reading `@v` can land on different connections and the read returns NULL. This applies to the query vector and to the API key alike, and it is intermittent: it only fires once more than one connection is free, so it passes in development and fails under load. Pin one connection with `Connx` and `defer conn.Close()` for the whole sequence. Prefer that over a transaction, because search performs no write and must stay runnable under a read-only account. The same hazard is why `store.Open` sets the session time zone as a connection-string parameter rather than with `SET`. Pinned by a live test.
- Selecting an `SVECTOR` column into Go is safe. It arrives as `[]byte` holding the text form, under both the binary and text protocols. Measured, not assumed.
- The bundled `villagesql` wrapper starts `mysqld` with `--no-defaults`, so `SET PERSIST` never survives a restart. Pass server flags at start: `villagesql --dir <dir> start -- --vsql_allow_preview_extensions=ON`.
- `veb_dir` depends on how the server was started. A wrapper-managed instance reads `<instance>/veb/` and begins with no extensions at all; the bundled ones must be copied in.
- `vsql status` lies on a default install: it pings without credentials, is denied, and reports the server down. Check liveness with an actual query instead.
- The control script is on PATH as `vsql` (a symlink created by hand). The `villagesql` command on PATH is the MySQL client, not the control script, despite the documentation using that name for the script.
- `vsql-vector` has no releases. Its `main` does not build against SDK 0.0.6. Pinned at commit `ec2282c`. Re-choose that pin by hand on any SDK upgrade.
- `ai_prompt` and `ai_embedding` hold a server thread for the whole outbound HTTP call, one request per row, serially. Set `SET SESSION max_execution_time` accordingly.
- **Both return NULL plus `Warning 3200` on failure. They do not raise.** Check for NULL on every call. A half-failed embedding run writes NULLs and reports success.
- `ai_prompt` takes one flat prompt string. No system role, no messages array, no streaming, no token usage, no model fallback.
- API keys are plain function arguments, visible in query logs, slow query logs, and process lists. Hold them in a session variable, never inline in the statement, on the same pinned connection as the statement that reads them.
- An administrator's include/exclude judgement lives against the **address**, not the page, so a new candidate inherits it with nothing to carry forward.
- **Never a time function whose result depends on the session zone.** `CURRENT_TIMESTAMP` on a `DATETIME` evaluates in the session's zone and stores that wall clock verbatim, so two connections write different values for one instant. No column defaults to it. `UTC_TIMESTAMP` names its zone and is not that hazard.
- **Times the application has are written by the application, in UTC**, so that values compared against each other come from one clock. Three kinds of value take the server's clock instead, each because there is no application value to use, and each is a `UTC_TIMESTAMP(6)` written into the statement rather than a column default: a value that exists only to be compared against the server's own clock, which is the fetch lock's start time; a value written by a SQL file the application passes no parameters to, which is a migration's record of itself; and a fixture in a live test, where the instant is irrelevant to what is being asserted. Anything outside those three is a bug, not a fourth case.
- `information_schema.COLUMN_TYPE` reports `SVECTOR` without its width. Use `SHOW CREATE TABLE` when verifying the declared dimension; the width is enforced, just not introspectable that way.
- **DDL is not transactional.** `ROLLBACK` does not undo a `CREATE TABLE`. Migrations get idempotence instead of atomicity: declare each table with its indexes, constraints and generated columns inline in one create-if-absent statement.
- MySQL cannot index a `TEXT` column without a key length. URL columns are `VARCHAR(768)`, the utf8mb4 index limit.
- Publish is one transaction. That requirement is why everything lives in one database.

## Ingest hazards

Each produces a knowledge base that looks healthy and answers confidently while wrong. None are self-announcing. This is the list. `PLAN.md` has the site survey: the read-only checklist that tests for each one against the real site, and the record of what the current target answered. Run it before first publish and after any change to the site's shape.

1. **Client-side rendered content.** Site builders assemble FAQ blocks, carousels, and dynamic lists in the browser. A downloader gets empty containers inside a normal-looking page. Assert `required_strings` from `siteconfig.toml` and fail the run on a miss.
2. **Pages about other people or businesses.** Guest posts, customer stories, profiles of researchers the site owner writes about. Well-formed, on-brand, and not about the site owner. No rule detects this; exclude by URL group during review.
3. **A single unrepresentative price.** Most marketing sites publish exactly one figure. Every cost question retrieves it. Grep the downloaded text for currency symbols before publishing.
4. **Sections hidden from readers but present in HTML.** Conditional and unpublished blocks carry a hiding class and keep their text. Strip them or the bot quotes invisible content.
5. **Nav, footer, and sidebar repeated site-wide.** Unstripped, they enter every chunk and make unrelated pages score high. A digital garden adds a backlinks and graph-view block to this.
6. **Alias stubs.** Addresses whose only purpose is to point at the real page. They inflate the crawl and produce near-empty near-duplicate documents. Do not expect a redirect: a stub can return success carrying only a refresh instruction, in which case following redirects removes none of them. Do not expect capitalisation to mark them. Identify them by shape and by an instruction not to index, check whether their targets are already listed separately, and drop anything under a body-text floor.
7. **A sitemap that lists robots-disallowed paths.** Sitemap membership is not a permission grant. Filter the frontier against robots.txt explicitly.
8. **Unlinked services.** A login area on an address the public site never links. Describe from published copy if asked; never publish the address.
9. **Required topics with no source.** Check page text, not URLs. Content often lives in a page whose address says nothing about it.
10. **A sitemap that is not clean input.** Entries can be relative strings rather than addresses, can repeat, and can carry characters needing escaping. Measured: a relative entry containing a colon parses without error, the text before the colon becomes a scheme, and resolving it against the site base returns the string unchanged. Validate every entry and reject a malformed one loudly.
11. **Body text extracted by pattern rather than by parser.** A `>` inside an attribute value ends a naive tag match early, so attribute contents including script source leak into the extracted text and are then embedded and retrieved. Extract with a real HTML parser, never with a pattern.
