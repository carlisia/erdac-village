# Porting notes

Every place Erdac and Erdac Village diverge, and why. This is the record of what the stack change actually cost.

Each entry says what Erdac does, what this port does instead, and whether the difference was **forced** by the stack or **chosen**. Where a gap could be closed upstream, it links to the matching entry in [CONTRIBUTIONS.md](CONTRIBUTIONS.md).

Entries marked *unmeasured* were written from research and have not yet been confirmed against a running server. The opening spike converts them, or corrects them.

## Answering

### No streamed answers -- forced, unmeasured

Erdac streams the answer over SSE, emitting a `token` event per fragment and driving the page through `thinking`, `streaming`, `complete`. `ai_prompt` is synchronous and returns the whole completion after up to 30 seconds, so the response becomes a single JSON payload and the visitor watches a spinner instead of watching text arrive. The `streaming` body state becomes dead markup.

No fallback was written. Re-framing one response as fake SSE chunks would invent motion that does not exist. See CONTRIBUTIONS: streaming variant.

Erdac's `readAnswer` auto-detects framed versus plain text from the first non-empty buffer, so the copied frontend is expected to accept this with no edits. That expectation is unverified.

### No system role -- forced

`ai_prompt` takes one flat prompt string. Erdac sends a system prompt, then up to ten history turns carrying roles, then the question. Everything is flattened into labelled sections in one string.

This is the riskiest divergence in the port. The `[[NO_ANSWER]]` sentinel instruction lives in the system prompt, and the whole declined-versus-answered accounting depends on the model emitting it as the first token. An instruction demoted out of the system role may be weighted lower.

All 14 prompt tests carry over. The refusal rate is measured before and after against the question fixtures, so the drift is a number rather than a worry. See CONTRIBUTIONS: system parameter.

### No model fallback -- forced

Erdac sends an ordered pair of models and lets the provider pick. `ai_prompt` takes one model and has no fallback concept, so `chat_fallback` is dropped from configuration. See CONTRIBUTIONS: ordered model list.

### Errors do not raise -- forced

`ai_prompt` and `ai_embedding` return SQL NULL plus `Warning 3200` on failure; the statement does not abort. An errored outcome has to be inferred from a NULL rather than caught, and the provider's error text is truncated to 255 bytes. See CONTRIBUTIONS: strict mode.

### Token counts are gone, and so are their columns -- forced, then chosen

`ai_prompt` returns only a string. Erdac records `prompt_tokens` and `completion_tokens` on every query-log row and emits `llm.token_count.prompt` and `llm.token_count.completion` to Phoenix.

The columns and span attributes are removed rather than left NULL. That was a deliberate call: the alternative kept an empty column in the schema as a self-documenting reminder of the gap. With them gone, the CONTRIBUTIONS entry is the only record that this was ever possible. See CONTRIBUTIONS: usage reporting.

### The model call site -- chosen

Prompt assembly stays in Go; only the model call goes into SQL. Full-SQL retrieval-augmented generation -- retrieval, gate, prompt assembly via `GROUP_CONCAT`, and `ai_prompt` in one statement -- was evaluated and set aside, because assembling a prompt in SQL exercises MySQL string functions rather than VillageSQL, and the prompt contract is checkable in Go and effectively not in `GROUP_CONCAT`.

## Retrieval

### No index -- forced, unmeasured

Erdac uses an HNSW index over pgvector with `SET LOCAL hnsw.ef_search = 100`. `vsql_vector` has no index at all: extension-defined index types are not yet supported by the server, so every search is a sequential scan. Performance is explicitly out of scope here, and one site produces a corpus small enough that scanning all of it per question is not noticeable. See CONTRIBUTIONS: custom index types.

### The two extensions do not compose: an embedding cannot be stored from SQL -- forced, measured 2026-09-07

This is the sharpest finding of the port. `ai_embedding()` returns a string; an `SVECTOR` column holds a custom type; and there is no path from one to the other inside the database.

Every route was tried against `vsql_ai 0.0.6` and `vsql_vector` at `ec2282c`:

| Attempt | Result |
|---|---|
| `SET vec = ai_embedding(...)` | `Incorrect SVECTOR value` -- the string carries the binary charset |
| `SET vec = CONVERT(ai_embedding(...) USING utf8mb4)` | `cannot implicitly cast string expression. Use explicit conversion` |
| `SET vec = CAST(... AS SVECTOR(3072))` | syntax error; that cast does not exist |
| `SET vec = SVECTOR::FROM_STRING(CONVERT(...))` | `Cannot implicitly cast from SVECTOR to SVECTOR(3072)` |
| `SVECTOR::FROM_STRING(x, 3072)` | wrong number of arguments; it takes exactly one |
| declaring the column as bare `SVECTOR` | `Type requires a length specification` |

`FROM_STRING` produces a **dimensionless** `SVECTOR`. A column **requires** a dimension. Nothing converts between them. Direct string assignment demands a constant, which a function result is not.

**Consequence.** Embeddings must round-trip through the client. Go issues `SELECT ai_embedding(...)`, receives roughly 39,000 characters of JSON array, and writes it back as a **string literal** in the following `INSERT`. Two round trips and about 39 KB of SQL text per page chunk. The embedding still originates inside the database, so the ecosystem rule holds, but the composition their own stack implies does not exist.

### Search needs no write: `SVECTOR::FROM_STRING` accepts computed strings -- measured 2026-09-07

The text form of a vector accepts exponent notation on input, in both the column and the string constructor, and the server emits it on output for small elements: `0.000012` reads back as `1.20000004e-05`. Measured 2026-09-08. The store writes plain decimals anyway, which are also accepted; the choice predates the measurement and is kept because it is pinned by a test and costs a few characters per element.

`COSINE_DISTANCE` rejects a bare computed string, but accepts `SVECTOR::FROM_STRING(anything)`. Measured: a literal works, `CONCAT(...)` works, a user variable works, and `ai_embedding`'s output wrapped in `CONVERT` and `FROM_STRING` works.

So the search path is:

```sql
SET @q = CONVERT(ai_embedding('google','gemini-embedding-001',@k,?) USING utf8mb4);
SELECT id, 1 - COSINE_DISTANCE(vec, SVECTOR::FROM_STRING(@q)) AS similarity
  FROM chunks ORDER BY similarity DESC LIMIT ?;
```

**No write occurs.** The decision to store each question's embedding on its conversation-turn row is therefore unnecessary and is open for the third time. Search can run against a read-only database user.

Both statements must run on the same connection. A user variable belongs to one connection, and Go's pool hands out an arbitrary free one per call, so a search that issues these two through the pool can set the variable on one connection and read it on another, where it is NULL. Measured 2026-09-08: a variable set on one connection reads as NULL on another. Pin the connection for the pair. A bound parameter in place of the variable, which would remove the need to pin anything for the vector, is refused: measured the same day, `SVECTOR::FROM_STRING(?)` fails the way the recorded mechanism predicts, so the variable and the pin are both required.

`SVECTOR::FROM_STRING` appears in no documentation page. The extension's own README still describes this form as failing, a limitation removed by server issue #486 in May 2026 and never documented since.

The distinction that matters, and that cost this port two reversals: inference of a parameterized type runs at `fix_fields` time, so it succeeds for literals and user variables and fails for a function result. Search works because the embedding can be put in a user variable first. Storage fails because the column assignment has no inferred dimension to check against.

Measured end to end on 2026-09-07: three sentences embedded and stored at 3072 dimensions, and the question "How do I see my agents and their results?" ranked the portal sentence at 0.7407 against 0.4702 and 0.4689 for two unrelated sentences.

### The sentinel survives prompt flattening -- measured 2026-09-07

The largest risk in the port did not materialise. With the system role gone and all instructions flattened into one labelled string passed to `ai_prompt('anthropic', ...)`, an extract that covered the question produced a normal grounded answer, and an extract that did not produced exactly `[[NO_ANSWER]]` and nothing else.

This is a two-case result, not a refusal-rate measurement. The full fixture-set comparison still stands as planned work.

### A different embedding model, and a vector at the ceiling -- forced, partly measured

Erdac embeds with `openai/text-embedding-3-small` at 1536 dimensions through OpenRouter. OpenRouter is out of scope for this project, and `ai_embedding` does not offer an `anthropic` provider for embeddings, so the provider becomes `google`.

`gemini-embedding-001` defaults to 3072 dimensions and supports 768 and 1536 through Matryoshka truncation. **`ai_embedding(provider, model, api_key, text)` has no options parameter**, so the default is the only reachable value. `VECTOR_MAX_DIMENSION()` returns 3072, measured, so the column sits exactly at the maximum with no headroom, and the 1536 that would have matched Erdac is unreachable. See CONTRIBUTIONS: provider options.

`similarity_threshold = 0.25` was derived from 28 questions about a different website scored by a different model. It carries no evidence here and ships marked untuned.

### The database now holds a cloud API key -- forced

`ai_embedding` and `ai_prompt` both take the API key as a plain function argument. Their own README warns it may be visible in query logs, slow query logs, and process lists, and lists environment-variable support as a future enhancement. Keys are held in session variables so they stay out of statement text, which is mitigation rather than a fix.

Erdac read its key from an environment variable in the application process and never put it in a query. Note that this is not a new exposure introduced by the choice of Google: `ai_prompt` had the same shape for the Anthropic key already.

### Embeddings are generated one row at a time -- forced

Erdac batches 64 texts per HTTP request. `ai_embedding` issues one request per row, serially, holding a server thread for up to 30 seconds per call on a cloud provider. One round trip per chunk, and a page yields several chunks, so a full ingest is a long sequence of them. Performance is out of scope, but `SET SESSION max_execution_time` has to accommodate it. See CONTRIBUTIONS: batched embedding.

## Ingest

### Embedding runs one chunk at a time, inside the database -- forced, measured 2026-09-08

Erdac sends every chunk from every page to a provider in one request and gets one vector per chunk back, paired by position. Here the database function embeds one chunk per call, on one pinned connection with the key in a session variable, and reports failure by returning NULL with a warning rather than by raising. So the pipeline calls it once per chunk, checks every result, and treats a page with any refused chunk as a failed page that is not stored at all. Pairing by position cannot go wrong because there is no position; the cost is one round trip per chunk rather than one per site, and the provider is called serially where the original called it in a batch. Measured with the provider key injected: the store's live tier called the function through the pinned connection with the key in a session variable and read back a vector of the declared width, and a refused key produced NULL with a warning that reached the caller as the sentinel's message.

### Tokens are estimated from words -- chosen, unmeasured

Erdac counts tokens with the exact tokenizer its embedding model uses. This model's tokenizer has no Go implementation, so a chunk's tokens are its word count times a configured factor, rounded up. The floor and ceiling are therefore soft targets; the provider's input limit is enforced as a hard cap that a chunk may not exceed. An oversized section is cut at word boundaries rather than token boundaries, and the text between the words is kept as written, so paragraph breaks and heading lines inside a cut survive it as they did in the original. Unmeasured: the factor is inherited with its derivation and has not been checked against this provider's own counter. See the plan's note on the two inherited constants.

### The fetch lock is enforced, not advisory -- chosen, measured 2026-09-08

Erdac's lock is a row a run claims and releases; nothing checks it on a write. Here the one method that writes a candidate refuses a run that no longer holds the lock, under a shared lock on the row, a running crawl renews the lock on an interval, and taking over an abandoned lock is recorded so publish refuses until a force fetch completes. All three were added after a review found that the advisory lock let a slow crawl's pages mix with the next run's, and the decision of 2026-09-08 on what to do with a crashed crawl's pages settled the third. Measured in the live tier end to end.

### Three frontier rules the original never needed -- chosen, measured 2026-09-08

Erdac's frontier is configured exclusion patterns and the administrator's review decisions. Here three rules precede them, each from the survey of this site: an entry that is not an absolute address is skipped and named by its exact text, an address matching a configured robots pattern is skipped, and an address with a single path segment is skipped as an alias stub. The last replaces a capitalisation rule the configuration carried, which the survey measured catching about a quarter of the stubs sampled.

## Schema

### Partial unique indexes become generated columns -- forced, vendor-prescribed, measured 2026-09-07

Erdac allows one live and one pending row per address, enforced by `UNIQUE (url) WHERE state = 'candidate'` and its published twin. MySQL has no partial index. VillageSQL's own guide prescribes a generated column that holds the URL for one state and NULL otherwise, with a plain unique index on it, exploiting the fact that MySQL unique indexes permit unlimited NULLs.

One hazard comes with it: `ON DUPLICATE KEY UPDATE` fires on any unique collision and cannot name an index, and MySQL's behaviour is undefined when one insert collides on several.

That hazard cannot fire here, and the reason is structural rather than probabilistic. `state` holds exactly one value per row, so at most one of the two generated columns is ever non-NULL, so no single insert can collide on both indexes.

Measured on 2026-09-07 against the real schema, all nine cases behaving as required:

- one published and one candidate row for the same address coexist
- a second candidate for that address is rejected by `documents_one_candidate_per_url`
- a second published row is rejected by `documents_one_published_per_url`
- any number of superseded rows for one address are permitted
- no row ever populates both generated columns
- `ON DUPLICATE KEY UPDATE` run twice against one address updates in place and leaves one row
- an upsert of a candidate leaves the live published row for that address untouched
- the publish transaction, retiring before promoting, produces exactly one published row
- promoting **before** retiring fails with a duplicate-key error

That last case matters: it is the same failure Erdac hit in production, and MySQL enforces the ordering exactly as Postgres did.

### Migrations cannot be transactional -- forced, measured 2026-09-07

Erdac's migrations are each wrapped in one transaction, so a failure leaves the schema untouched. Postgres has transactional DDL; MySQL does not. Measured: `START TRANSACTION; CREATE TABLE a (...); ROLLBACK;` leaves the table in place, and a failed second statement does not undo the first.

The guarantee is replaced by idempotence, which is weaker: a partly-applied migration is recoverable by re-running rather than prevented. Every table is declared in a single create-if-absent statement carrying its indexes, constraints and generated columns inline, so no statement can apply without its dependencies, and seed rows are written in a repeatable form.

### A column cannot be added conditionally in one statement -- forced, then chosen, measured 2026-09-09

Erdac declares its whole schema in one file of create-if-absent statements and never alters a table; had it needed to, Postgres accepts `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`. The second migration here adds a column to an existing table, and MySQL refuses that form: `ADD COLUMN IF NOT EXISTS` is a syntax error (1064), and a plain `ADD COLUMN` run a second time fails with `Duplicate column name` (1060). Both measured 2026-09-09.

The forced part is the missing syntax. The chosen part is what stands in for it. The migration asks the catalogue (`information_schema.columns`, the server's own table of every column) whether the column is present, builds the `ALTER` as a string only when it is not, and runs that string through `PREPARE` and `EXECUTE`, the server's mechanism for executing a statement held in a variable. Upstream MySQL's manual lists `ALTER TABLE` among the statements a prepared statement may hold, which is why the shape works. VillageSQL's own schema-migrations guide does not prescribe it; that guide prescribes a migrations table so reruns are safe, which this repository also has. So this entry does not carry the vendor-prescribed label that the generated-column entry above carries.

The guard is kept even though the migrator already skips recorded versions, because DDL is not transactional (see `Migrations cannot be transactional` above): a migration that ran its `ALTER` and died before recording itself is recorded nowhere and, without the guard, fails on every retry with error 1060. A version record alone, which is what the vendor's guide describes and what Flyway and golang-migrate do, leaves that half-applied case to be repaired by hand. `TestEveryAlterIsPreparedConditionally` refuses any migration file whose `ALTER` is not inside a prepared string, and `TestLiveTheSecondMigrationAddsItsColumnOnce` applies the file twice against a real server and finds the column once.

### A URL column cannot be TEXT -- forced, measured 2026-09-07

Erdac declares `url text` and indexes it directly. MySQL refuses: `BLOB/TEXT column 'url' used in key specification without a key length`.

The column becomes `VARCHAR(768)`, 768 being the largest utf8mb4 length that fits an index key. Addresses longer than that would be silently rejected at insert rather than truncated, so the ingest path treats an over-length URL as a failed page rather than storing a mangled one.

### Arrays become a child table -- forced, vendor-prescribed

`retrieved_chunk_ids bigint[]` and `scores real[]` become a `query_log_retrieval` child table holding log id, chunk id, score and rank. This is arguably an improvement: the parallel arrays always relied on a positional correspondence nothing enforced.

### Timestamps -- forced

`timestamptz` has no MySQL equivalent. `DATETIME(6)` is used, with UTC written and read explicitly, rather than `TIMESTAMP`, which converts on read according to session timezone -- the admin portal displays fetch and publish times, where a silent shift would be invisible and wrong.

### RETURNING and DISTINCT ON -- forced, vendor-prescribed

Four `RETURNING` sites become `LAST_INSERT_ID()` after an insert, or a `SELECT` after an update inside the same transaction. The latter is safe only because those two updates target a single-row table inside a transaction; the same rewrite applied to a multi-row update would be a race.

`DISTINCT ON (url)` becomes `ROW_NUMBER() OVER (PARTITION BY url ...)` filtered to 1. `ANY(%s)` becomes generated placeholders in an `IN` list.

### Chunk identifiers cost one round trip each -- forced, unmeasured

Erdac inserts all of a page's chunks in one statement and reads their identifiers back from `RETURNING id`. It needs those identifiers because the vector for a chunk is stored against the chunk's row.

MySQL has no `RETURNING`, and the substitute for it does not survive a multi-row insert. `LAST_INSERT_ID()` reports the identifier of the first row of a batch, and consecutive identifiers for the rest of the batch are only guaranteed under one of the server's three automatic-identifier allocation modes. The default is not that one. So a batched insert would return identifiers that are correct on many servers and silently wrong on others, and wrong identifiers here attach a page's vectors to another page's chunks.

Chunks are therefore inserted one at a time, each reporting its own identifier. The cost is one round trip per chunk instead of one per page, paid on every fetch of every changed page.

Marked unmeasured: this rests on the server's documented behaviour for its allocation modes, not on an observed failure. Measuring it means setting the interleaved mode and showing a batch return a non-consecutive identifier.

## Tooling and tests

### Selecting an SVECTOR into Go is safe -- measured 2026-09-07

An earlier revision of this plan assumed `go-sql-driver/mysql` would hard-error on an `SVECTOR` column, because its binary protocol ends its field-type switch with an error on unknown types rather than degrading to bytes. The design was shaped to avoid ever reading a vector into Go.

Measured, it works. `SELECT id, vec FROM v` returns `[]byte "[1,0,0,0]"` under both the binary and text protocols, with and without bound parameters present.

The constraint is lifted. It is recorded because the avoidance it caused is visible in the data model discussion above, and a later reader should know the design was not shaped by a real limit.

### The fast test tier no longer covers the product -- forced

All 214 Erdac tests run with no database and no network. Once retrieval, embedding and generation all live in SQL, a fake store stops standing in for storage and starts standing in for the product, so a test against it proves little.

Two tiers instead. Fakes and `go-sqlmock` cover what stays in Go -- chunking, prompt assembly, the sentinel contract, handlers, config -- and assert which statement was issued with which arguments. A second tier requires the live server for schema behaviour, generated-column uniqueness, the query-vector join and the `vsql_ai` calls, and skips when `TEST_MYSQL_DSN` is unset.

This is a real regression against Erdac and it is recorded as one. `check.sh` reports which tier skipped, so a green run never quietly means half of it did not execute.

### testcontainers is unusable -- forced

Testcontainers requires a Docker-API-compatible runtime and offers no supported way to point at an already-running native server. Running VillageSQL natively rules it out.

### The tokenizer is deliberately approximate -- chosen

Erdac counts chunk tokens with `tiktoken` and `cl100k_base`. Google's embedding models do not use that tokenizer, and Go has no maintained implementation of the one they do.

Counting moves to a word-count approximation whose divisor is calibrated once against Google's `countTokens` endpoint and hard-coded with its derivation. Chunk sizing needs a monotonic, stable measure rather than an exact one: being consistently off by a fixed factor shifts every boundary equally. Calling a remote endpoint per measurement would turn a pure function into a network call inside the chunk-splitting loop and break the offline test tier.

One consequence is sharper than it was with a local model. `gemini-embedding-001` rejects input over 2048 tokens outright, so an approximation that under-counts can produce a chunk the provider refuses. The calibration divisor is chosen to over-count rather than under-count, and the ingest run asserts the largest produced chunk against the real limit before embedding anything.

### A pre-1.0 telemetry dependency -- chosen

`openinference-semantic-conventions` for Go is v0.1.x on a repository that declares Python the source of truth and checks Go against it in CI, and it currently pins an older OpenTelemetry SDK than the current release. It is used anyway, because the attribute keys are the entire value and hand-copied constants are how one key ends up misspelled and silently absent from Phoenix.

## Not ported

The whole of `infra/` except its intent: no deployment wizard, no DNS, no Vercel, no env sync, no remote verification. Local equivalents exist for secrets staying out of the repository, a preflight, and a teardown.

## Build and operations

### `vsql_vector` does not compile against the released SDK -- measured 2026-09-07

`vsql-vector` has **no tags and no releases**, only `main` and twelve personal branches. Its `main` fails to build against the shipped `villagesql-extension-sdk-0.0.6`:

```
graph.cc:292: error: too many arguments to function call, expected 2, have 3
    m_index.get_key_ref(VECTOR_KEY_POS, data.data, &owner_ref)
```

The timeline explains it. The 0.0.6 dev-server and SDK were built 2026-08-24. The HEAD commit of `vsql-vector`, dated 2026-08-25, is titled *"HNSW Index: Adapt to Key API change (#54)"*. The extension was adapted to a post-0.0.6 interface the day after the SDK was cut, and there is no released version of the extension that matches the released SDK.

**This build pins `vsql-vector` to commit `ec2282c`**, the commit immediately before that adaptation, which compiles and loads cleanly. That pin is fragile: it is a commit hash on a branch with no release discipline, and any SDK upgrade will need it re-chosen by hand.

Note also that the extension's README says HNSW support is "planned" and search is a sequential scan today, while `src/index/hnsw/` is core to the library and five active branches work on it. The documentation lags the code.

### `SET PERSIST` cannot survive a wrapper-managed restart -- measured 2026-09-07

VillageSQL's documentation says to enable preview extensions with `SET PERSIST vsql_allow_preview_extensions = ON`. The statement succeeds and the value is written into `datadir/mysqld-auto.cnf`, confirmed by reading the file.

It has no effect, because the bundled `villagesql` wrapper starts `mysqld` with `--no-defaults`, which suppresses reading that file. This affects every persisted variable, not just this one.

The working form is to pass the flag at start: `villagesql --dir <dir> start -- --vsql_allow_preview_extensions=ON`.

### `veb_dir` depends on how the server was started -- measured 2026-09-07

The install script starts a server whose `veb_dir` is `prebuilt/lib/veb/`. The `villagesql` wrapper starts an instance whose `veb_dir` is `<instance>/veb/`. `villagesql veb add` writes to the instance directory.

The consequence is a silent failure mode: run the installer, then `veb add`, then use the running server, and the extension is invisible. Worse, an instance started by the wrapper has none of the thirteen bundled extensions either, `vsql_ai` included, until they are copied across.

### The install script sends telemetry -- measured 2026-09-07

`https://install.villagesql.com` posts install started, completed and failed events to `api.mixpanel.com`, carrying an anonymous per-machine UUID plus OS, architecture, install method, codebase and version. No documentation page mentions this.

Opting out is undocumented outside a source comment: create an empty `~/.villagesql/.install_id` before installing. This repository opted out.
