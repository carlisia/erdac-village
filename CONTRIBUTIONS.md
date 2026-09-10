# Contributions

Gaps in VillageSQL and its extensions that this port ran into, and what would close each one upstream.

This is aimed outward, at the project rather than at this repository. [PORTING.md](PORTING.md) records what the gap cost here; this file records what would fix it for everyone.

Every entry below was found by reading the projects' own documentation and source. None has yet been confirmed against a running server -- the opening spike does that, and entries move from *researched* to *measured* as it goes. Nothing should be filed upstream while still marked researched.

## Strongest candidates

### 1. `ai_prompt` should accept a system prompt, or a messages array

**Status:** researched. **Repo:** `villagesql/vsql-ai`.

`ai_prompt(provider, model, api_key, prompt)` takes a single flat string. Every current chat model API distinguishes a system instruction from user turns, and models weight the two differently. Any retrieval-augmented application has instructions that belong in the system role -- grounding rules, refusal contracts, tone -- and flattening them into the user turn measurably weakens adherence.

This affects every RAG user of the extension, not one project. A fourth argument for a system string would cover most of it; a messages array would cover all of it and also give multi-turn conversations somewhere to live.

Worth pairing with a measurement rather than an assertion: refusal-rate before and after flattening, on a fixed question set.

### 2. `ai_prompt` should return token usage

**Status:** researched. **Repo:** `villagesql/vsql-ai`.

The function returns only the completion text. Providers return prompt and completion token counts alongside it, and the extension discards them.

Without usage, nobody running `vsql_ai` can attribute spend at all -- not per query, not per table, not in aggregate. For a feature whose own documentation warns that `SELECT ai_prompt(...) FROM t` issues one billable request per row, serially, that is a sharp omission: the extension makes it easy to spend a lot and impossible to see how much.

A second output column, an out-parameter, or a companion status variable would all work.

### 3. Failures should be able to raise instead of returning NULL

**Status:** measured 2026-09-07. **Repo:** `villagesql/vsql-ai`.

Every failure path returns SQL NULL plus `Warning 3200`, and the statement continues. Provider error text is truncated to 255 bytes.

Silent degradation is the wrong default for a data pipeline. An `INSERT ... SELECT ai_embedding(...)` that half fails writes NULLs into an embeddings column and reports success; the corpus is then quietly incomplete and every later similarity search is wrong in a way nothing surfaces. A caller must remember to check for NULL and read `SHOW WARNINGS` on every statement, and forgetting is invisible.

Measured with a deliberately invalid key on `vsql_ai 0.0.6`:

```sql
CREATE TABLE emb (id INT PRIMARY KEY, v TEXT);
INSERT INTO emb VALUES (1, ai_embedding('google','gemini-embedding-001',@bad_key,'a'));
SELECT ROW_COUNT();          -- 1
SELECT v IS NULL FROM emb;   -- 1
```

The row is inserted, the value is NULL, `ROW_COUNT()` reports success, and the failure appears only as `Warning 3200: VDF error in function 'ai_embedding': API key not valid`. A caller that does not check both for NULL and for warnings records a successful ingest over a corpus with holes in it.

Proposal: a session variable, say `vsql_ai.strict`, that makes failures abort the statement. Keep the current behaviour as the default for compatibility.

A consequence for `vsql_mcp`, measured on each side and joined by inference: its `query` tool accepts exactly one read-only statement per call, and the function reports failure as NULL plus a warning readable only by a following `SHOW WARNINGS`. An agent that calls `ai_embedding` or `ai_prompt` through the tool therefore sees NULL and can never see why.

### 4. `vsql_vector`'s vector-argument rule is real, but described wrongly

**Status:** measured 2026-09-07. **Repo:** `villagesql/vsql-vector`.

The README frames the limitation as missing constant folding, implying a literal fails. A literal is in fact the one inline form that works. Measured, `COSINE_DISTANCE` accepts only a true constant or a custom-type column: a bound parameter fails, `CONCAT(...)` fails, a user variable fails, a literal works, and a subquery returning a constant works.

Two costs follow from the wording. A reader designs around the wrong constraint, as this port did. And the case that actually bites, a bound parameter over the binary protocol, is the one every database driver uses by default, so a user reaches it immediately and the README does not name it.

Two things would close this. Correcting the wording, with the table of accepted and rejected forms. And accepting a bound parameter, which is the form users actually need: `ORDER BY COSINE_DISTANCE(embedding, ?)` is the query every vector-search application wants to write, and today it is the one form guaranteed to fail.

Worth noting the knock-on: because a function result is not a constant either, `COSINE_DISTANCE(col, ai_embedding(...))` cannot work, so the two extensions in this vendor's own stack cannot be composed in a single expression. Fixing the parameter case fixes that too.

### 5. `ai_embedding` should expose provider options, starting with output dimensionality

**Status:** researched. **Repo:** `villagesql/vsql-ai`.

`ai_embedding(provider, model, api_key, text)` has four arguments and no options parameter, so a model's tunable settings are unreachable and you always get its default.

The concrete cost: `gemini-embedding-001` supports 768, 1536 and 3072 output dimensions through Matryoshka truncation, and defaults to 3072. `SVECTOR` caps at 3072. So a user of both extensions together is forced to the maximum supported width with no headroom, cannot match an existing 1536-wide corpus, and pays double the storage and double the scan cost of a 1536 embedding that would serve them as well.

This is not Google-specific. Output dimensionality, task type, and truncation behaviour are standard knobs across embedding providers, and none of them are reachable today.

A fifth argument taking a JSON object of provider options would cover all of them without another signature change later.

### 6. `vsql-vector` has no release that matches any released SDK

**Status:** measured 2026-09-07. **Repo:** `villagesql/vsql-vector`.

The repository has no tags and no releases. Its `main` does not compile against the shipped `villagesql-extension-sdk-0.0.6`, failing on a three-argument call to a `get_key_ref` the SDK declares with two.

The cause is visible in the history: the SDK was built 2026-08-24, and HEAD is dated 2026-08-25 and titled "HNSW Index: Adapt to Key API change (#54)". The extension tracks an unreleased server interface.

The practical result is that a user following the documentation cannot build the extension at all, and the only way through is to read the log, guess which commit predates the break, and pin it. This port pinned `ec2282c`.

Since the extension builds against the dev ABI, which requires an exact protocol match rather than a minimum, this will recur on every SDK release. A tag per SDK version, or a compatibility table in the README, would close it.

### 7. `SET PERSIST` silently does nothing under the bundled `villagesql` wrapper

**Status:** measured 2026-09-07. **Repos:** `villagesql/villagesql-server`, `villagesql/villagesql-docs`.

The documentation instructs users to enable preview extensions with `SET PERSIST vsql_allow_preview_extensions = ON`. The statement succeeds and the value is written to `datadir/mysqld-auto.cnf`.

The bundled `villagesql` wrapper starts `mysqld` with `--no-defaults`, which suppresses reading that file, so the setting is silently discarded on the next restart. This applies to every persisted variable, not only this one.

Nothing warns the user. The variable simply reads OFF again and `INSTALL EXTENSION` fails with a message about preview capabilities, pointing at the symptom rather than the cause.

Either the wrapper should stop passing `--no-defaults`, or it should read `mysqld-auto.cnf` explicitly, or the documentation should tell users to pass the flag at start instead.

`vsql_mcp` is the sharpest instance, measured 2026-09-08: every one of its settings, including which schema it exposes, the account it queries as and the token it requires, is a `SET GLOBAL`. After a restart under the wrapper the MCP listener is off and a client's first symptom is a refused connection with nothing saying why.

### 8. `veb_dir` differs between the installer's server and the wrapper's instance

**Status:** measured 2026-09-07. **Repo:** `villagesql/villagesql-server`.

The install script starts a server reading `prebuilt/lib/veb/`. The `villagesql` wrapper starts an instance reading `<instance>/veb/`, and `villagesql veb add` writes there.

Install, then `veb add`, then connect to the server the installer left running, and the extension is simply absent with no indication why. A wrapper-started instance also begins with none of the thirteen bundled extensions, `vsql_ai` included.

One canonical location, or a warning when the two disagree, would prevent a confusing first hour.

### 9. The install script's telemetry is undocumented

**Status:** measured 2026-09-07. **Repo:** `villagesql/villagesql-server`.

The installer posts install started, completed and failed events to `api.mixpanel.com` with an anonymous per-machine UUID, OS, architecture, install method, codebase and version. The payload contains no personal data.

The issue is disclosure, not content. No documentation page mentions it, and the opt-out, creating an empty `~/.villagesql/.install_id`, exists only in a source comment. A line in the install documentation and a named environment variable such as `VSQL_NO_TELEMETRY` would settle it.

### 10. `villagesql status` reports every default install as down

**Status:** measured 2026-09-07. **Repo:** `villagesql/villagesql-server`.

The control script's `status` command checks liveness with `mysqladmin --socket=... ping`, passing no user and no password. The installer sets a generated root password by default, so the ping authenticates as the invoking OS user, is denied, and `status` prints "process running but not accepting connections yet" and exits non-zero.

The server is fine. The same socket answers queries immediately. But the one command a user runs to ask "is it up" says no, on a stock install, every time, and its wording points at a startup delay that is not happening.

Reading the credentials file the installer already wrote, or accepting an `Access denied` response as proof the server is answering, would both fix it. The second is the standard trick: a rejected login still proves a live server.

### 11. Two different programs on PATH are both called `villagesql`

**Status:** measured 2026-09-07. **Repo:** `villagesql/villagesql-server`.

The installer creates `~/.local/bin/villagesql` as a symlink to `bin/mysql`, the interactive client. The lifecycle control script, which owns `start`, `stop`, `status`, `veb` and `mysql-test`, is a separate file also named `villagesql`, at the package root, and is not placed on PATH at all.

So the command named `villagesql` is not the program the documentation calls `villagesql`. A user who types `villagesql start` gets the MySQL client attempting to open a database named "start". Adding the package root to PATH does not help either, because then two files named `villagesql` compete and PATH order decides.

Naming the client shortcut `villagesql-client`, to match the existing `villagesql-admin` and `villagesql-server`, and putting the control script on PATH as `villagesql`, would resolve it without changing any documented command.

### 12. Nobody has walked the seam between `vsql_ai` and `vsql_vector`

**Status:** measured 2026-09-07, and triaged against the project's own issues. **Repos:** `villagesql/vsql-ai`, `villagesql/vsql-vector`.

**This is not a new bug report. Every component is already known and scheduled.** What is missing is anything connecting them to embeddings.

The composition fails because a runtime-computed string cannot be assigned to a custom-type column. That refusal is deliberate and has its own test directory, `mysql-test/suite/villagesql/implicit_cast/disallowed/`. The intended replacement for casting is `FROM_STRING` with parameter inference, which runs at `fix_fields` time and is wired for literals and user variables but not for a function result. The gap is tracked as server #374 (parameterized-type disambiguation, milestone 0.0.8) and #400 (explicit cast syntax, milestone 0.0.7), with #204 an open user report of the same symptom against a different custom type since April.

So the useful contribution is not "fix the cast". It is to record that the flagship pairing does not work today, and to attach a concrete reproduction to issues that are currently phrased abstractly:

```sql
CREATE TABLE d (id INT PRIMARY KEY, vec SVECTOR(3072));
UPDATE d SET vec = SVECTOR::FROM_STRING(
  CONVERT(ai_embedding('google','gemini-embedding-001',@k,'text') USING utf8mb4));
-- ERROR 3219: Cannot implicitly cast from vsql_vector.SVECTOR
--             to vsql_vector.SVECTOR(3072) for column 'vec' at row 1
```

`vsql-ai#7` has asked for this composition since 2026-03-18 and is still open. It is the natural place to attach the reproduction.

Note that the 0.0.7 charset fix will **not** resolve this. Server #1054 touches only the JSON-function path; applying `CONVERT` by hand, which is what 0.0.7 does automatically, merely moves the error from `Incorrect SVECTOR value` to `cannot implicitly cast string expression`.

### 13. `vsql-vector`'s README documents a limitation that no longer exists

**Status:** measured 2026-09-07. **Repo:** `villagesql/vsql-vector`.

The README states that `SVECTOR::FROM_STRING(...)` as a direct distance-function argument "currently fails because constant folding for parameterized custom types is not yet supported", and prescribes storing the query vector in a table row and joining against it.

Server issue #486 landed constant-string inference on 2026-05-14. The extension's own `mysql-test/t/vector_function.test:97` now tests `COSINE_DISTANCE(v, SVECTOR::FROM_STRING('[1.0, 0.0, 0.0, 0.0]'))` passing. Measured here, it works, and so do `CONCAT` and user-variable forms.

The README was never updated, and its example queries still carry `-- TODO: once inline constant vector support is added` comments for a feature that arrived four months ago.

The cost is concrete. A reader designs every search around a write, which rules out read replicas and read-only database users and adds a row lifecycle. This port did exactly that, twice, before measuring.

### 14. `SVECTOR::FROM_STRING` is invisible where a vector user would look

**Status:** measured 2026-09-07. **Repos:** `villagesql/villagesql-docs`, `villagesql/vsql-vector`.

The `TYPE::method` syntax is properly documented in `custom-types.mdx`, including the rule that expressions resolving to STRING are not implicitly coerced and must be wrapped in `TYPE::from_string`. That guidance is written against `COMPLEX`, a non-parameterized type, where it works.

For parameterized types it is silent, and for parameterized types wrapping a runtime value in `FROM_STRING` is exactly what fails. A reader following the documented rule reaches an error the documentation does not predict.

Separately, `vsql-vector` appears in **no** documentation page. Grepping the docs repository for `SVECTOR`, `vsql_vector` and `FROM_STRING` returns nothing, and the extension is absent from the extension list on the website. The only embeddings guide stores vectors in a `JSON` column and directs similarity search to NumPy or Faiss, justified by the sentence "Neither MySQL nor VillageSQL provides a native vector distance function" -- which `COSINE_DISTANCE` makes untrue.

Two fixes, both documentation: extend the coercion rule to cover parameterized types, and correct the embeddings guide now that a distance function exists.

### 15. `information_schema.COLUMN_TYPE` drops a custom type's parameters

**Status:** measured 2026-09-07. **Repo:** `villagesql/villagesql-server`.

A column declared `SVECTOR(3072)` is reported by `information_schema.columns.COLUMN_TYPE` as `vsql_vector.SVECTOR`, with the width absent. `SHOW CREATE TABLE` reports it correctly as `vsql_vector.SVECTOR(3072)`.

`COLUMN_TYPE` is what schema tooling reads: migration verifiers, schema-diff tools, ORM introspection, and anything asserting that a live column still matches what a configuration file expects. All of them see a type they cannot distinguish from a differently-sized one.

The width is recoverable, but only by arithmetic on `character_maximum_length`, which reports 12296 for a 3072-element vector: the elements plus eight bytes of overhead. Requiring a tool to know an extension's per-element size and header length to recover a declared parameter is not an introspection API.

Including the resolved parameters in `COLUMN_TYPE`, as built-in parameterized types such as `VARCHAR(n)` and `DECIMAL(p,s)` already do, would close it.

Measured 2026-09-08: `vsql_mcp`'s `describe_table` tool reads the same catalogue and reports the column as `vsql_vector.SVECTOR` with no width, so an agent working through MCP cannot learn the declared dimension from the schema tool at all. A stored value's width is recoverable with `VECTOR_DIMENSION(col)`, measured the same day, but that is a property of a row, not of the declaration, and `LENGTH`, `CONVERT` and `JSON_LENGTH` all refuse a vector value with error 1221.

### 21. `vsql_mcp.schema` does not give the query tool a default database

**Status:** measured 2026-09-08. **Repo:** `villagesql/vsql-mcp`.

`schema` restricts what the resources and `list_tables` expose. The `query` tool runs on the connection named by `db_url`, and that connection has no default database unless `db_url` names one. The README's own quick start sets `schema = 'mydb'` and a `db_url` with no database, so a reader who follows it and then issues the obvious first query, `SELECT ... FROM sometable`, gets `ERROR 1046: No database selected`. `list_tables` works and shows the table; the query against it fails.

This bites every user of the quick start, and it bites agents worst: an agent follows `list_tables` with an unqualified query as a matter of course, and the error names nothing the agent has seen. The workaround is to put the schema into `db_url`, which the README does not say.

Either would close it: the tool issuing `USE <schema>` on its connection when `schema` is set, or the quick start and the `db_url` row of the configuration table saying the database must be named there.

## Real, lower priority

### 16. Custom index types, and then an HNSW index

**Status:** researched, and already acknowledged upstream. **Repos:** `villagesql/villagesql-server`, `villagesql/vsql-vector`.

The server's own Known Limitations state that extension-defined custom index types are not available, and `vsql_vector`'s README says HNSW support is planned and that search is a sequential scan today.

The dependency runs one way: the index cannot exist until the server supports the extension point. Worth tracking rather than proposing, since both projects already name it.

### 17. A streaming variant of `ai_prompt`

**Status:** researched. **Repo:** `villagesql/vsql-ai`.

The call holds a server thread for up to 30 seconds on a cloud provider and 300 on a local one, and returns nothing until it completes. Any interactive application built on it shows a spinner for the whole duration.

This one needs a design answer before a patch: SQL has no natural shape for incremental results from a scalar function. A table-valued function yielding chunks, or writing progressively into a row, are both plausible and both are a larger conversation than the entries above. Raise as a discussion, not a pull request.

### 18. `ai_prompt` should accept an ordered model list

**Status:** researched. **Repo:** `villagesql/vsql-ai`.

One model argument, no fallback. Providers and gateways commonly accept an ordered list and use the first that answers. Without it, a model outage is an outage.

### 19. `ai_embedding` should support batching

**Status:** researched. **Repo:** `villagesql/vsql-ai`.

One row is one serial HTTP request, with no batching and no concurrency, and the README says so explicitly. Every embedding provider accepts arrays of inputs; a set-returning or array-accepting form would cut both wall-clock time and request count by a large factor on any real corpus.

### 22. The schema-migrations guide stops one step short of a re-runnable migration

**Status:** measured 2026-09-09. **Repo:** `villagesql/villagesql-docs`.

The schema-migrations guide (`guides/schema-migrations.mdx`) says each migration tool "maintains a migrations table in the database to track which migrations have run, so reruns are safe." The transactions guide says a DDL statement (`CREATE TABLE`, `ALTER TABLE`, `DROP TABLE`) commits implicitly and cannot be rolled back. Neither page joins the two. A migration file holding more than one statement that fails between them has applied its DDL and is recorded nowhere, so the next run re-applies it. Measured: `ADD COLUMN IF NOT EXISTS` is a syntax error (1064), and a plain `ADD COLUMN` run again fails with `Duplicate column name` (1060). The rerun the guide calls safe fails on every attempt until someone repairs the database by hand.

This affects every user of the tools the guide lists, because Flyway, Liquibase, golang-migrate and Alembic all run files of several statements, and it bites hardest on the guide's own "add a nullable column" answer, the migration it presents as the easy case.

The fix is documentation, one section: state that MySQL has no `ADD COLUMN IF NOT EXISTS`, and show the shape that makes an `ALTER` re-runnable, a check of `information_schema.columns` followed by `PREPARE` and `EXECUTE` of the statement only when the column is absent. Upstream MySQL's manual lists `ALTER TABLE` among the statements a prepared statement may hold, so the shape needs no server change. This port uses it in its second migration and proves it by applying the file twice against a live server.

## Already fixed upstream

### 20. `ai_embedding` returns a binary-charset string

**Status:** measured 2026-09-07; fixed in 0.0.7, but only for JSON functions. **Repo:** `villagesql/vsql-ai`.

On 0.0.6 and earlier the returned string carries the binary charset, so JSON columns and JSON functions reject it with `ERROR 3144`. The workaround is `CONVERT(... USING utf8mb4)`.

It also breaks `SVECTOR` assignment, which is not documented anywhere, and there the error is `Incorrect SVECTOR value` -- which points at the value rather than at the charset and gives a user nothing to search for.

The 0.0.7 fix, server #1054, sets the runtime charset to match the declared one. Its diff touches `item_func.cc` and `sql_udf.h` and is framed entirely around JSON functions. **It does not resolve custom-type column assignment**, which fails for an unrelated reason: applying `CONVERT` by hand, which is what 0.0.7 now does automatically, only moves the error from `Incorrect SVECTOR value` to `cannot implicitly cast string expression`.

Recorded so that a `CONVERT` in this codebase has a reason attached, so it can be removed on 0.0.7, and so nobody expects that upgrade to fix storage.
