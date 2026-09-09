# 0001 - Schema and the review workflow

Status: ready. Written 2026-09-07, after the spike proved the mechanism.

## Problem Statement

An administrator needs to change what the assistant knows without ever letting an unreviewed page answer a visitor.

Today the repository has a configuration loader and nothing else. There is no place to put a page, no way to mark one approved, and no way to make approved pages go live together. Until that exists, every other part of the system has nowhere to write.

The requirement that makes this hard is not storage. It is that one **address** must be able to hold a **published** page and a **candidate** page at the same time. The assistant keeps answering from the published one while the administrator reviews the candidate. The original system enforced that with a Postgres feature MySQL does not have, and the substitute is not obvious.

A second requirement is unforgiving in a different way. **Publish** must be all-or-nothing. If it half-applies, the corpus contains a mixture of two crawls and nothing reports it.

## Solution

A schema, applied by numbered migrations, that holds pages through their whole life and enforces the review rules in the database rather than in application code.

An administrator fetches, sees a list of candidates, marks addresses **included** or **excluded**, and publishes. Until they publish, visitors see the old answers. After they publish, every included candidate is live at once and the pages it replaced are gone.

If two of anything would break the rules -- two candidates for one address, two published pages for one address -- the database refuses the write. Not a check in Go that someone can forget to call.

## User Stories

1. As an administrator, I want a fetched page to be stored as a candidate, so that it cannot answer a visitor before I have seen it.
2. As an administrator, I want an address to hold a published page and a candidate page at the same time, so that reviewing a change does not take the current answer offline.
3. As an administrator, I want the database to reject a second candidate for one address, so that two fetches cannot silently leave me reviewing the wrong one.
4. As an administrator, I want the database to reject a second published page for one address, so that a visitor's question cannot match two versions of the same page.
5. As an administrator, I want publish to promote every included candidate together, so that the corpus is never a mixture of two crawls.
6. As an administrator, I want publish to fail entirely rather than partially, so that a failure leaves me exactly where I started.
7. As an administrator, I want the pages that a publish replaced to be deleted, so that storage does not grow without limit and no superseded page can be retrieved.
8. As an administrator, I want to mark an address excluded, so that a page I do not want answering is neither fetched again nor searched.
9. As an administrator, I want exclusion to be sticky across refreshes, so that I do not have to re-exclude the same address after every fetch.
9a. As an administrator, I want my judgement recorded against the address rather than a page, so that no future code path can forget to carry it forward onto a new candidate.
10. As an administrator, I want an excluded address to stay excluded even if it is already published, so that exclusion takes effect immediately rather than at the next publish.
11. As an administrator, I want to see when the site was last checked and when it was last published, so that I can tell "nothing changed" apart from "nothing was reviewed".
12. As an administrator, I want to know how many pages and chunks are live, so that I can tell whether a publish did what I expected.
13. As an administrator, I want a fetch that is already running to block a second one, so that two crawls cannot interleave into one candidate set.
14. As an administrator, I want a stale fetch lock to expire, so that a crashed run does not lock me out permanently.
15. As an administrator, I want publish to be refused while a fetch is running, so that I cannot promote a half-written candidate set.
16. As an administrator, I want to halt the assistant, so that I can stop it answering without taking the site down.
17. As an administrator, I want to clear every open conversation, so that a visitor cannot continue from a state I no longer want.
18. As a visitor, I want the assistant to answer only from published, included pages, so that I am never shown something an administrator has not approved.
19. As a visitor, I want my question recorded when the assistant could not answer it, so that the gap can be found and filled.
20. As a visitor, I want my question recorded exactly once per asking, so that the administrator sees how often a gap is actually hit.
21. As an administrator, I want each unanswered question to carry why it failed, so that "nothing matched" is distinguishable from "the provider failed".
22. As an administrator, I want each unanswered question to carry the chunks that were considered and their scores, so that a threshold can be tuned against evidence.
23. As a developer, I want the schema applied by numbered migrations, so that a database can be brought from empty to current without guesswork.
24. As a developer, I want every migration to be idempotent, so that re-running one is safe.
25. As a developer, I want a partly-applied migration to converge when re-run, so that a failure part-way through is recoverable without hand-repair.
26. As a developer, I want a migration to detect what it must change rather than assume, so that it does not fail on a database that is already partly migrated.
27. As a developer, I want the review rules asserted by tests that try to break them, so that a future edit cannot quietly remove the protection.
28. As a developer, I want the schema tests to run against a real server, so that they test MySQL rather than my belief about MySQL.
29. As a developer, I want the fast test tier to keep running with no database, so that the ordinary edit-and-check loop stays fast.
30. As a developer, I want each caller to depend only on the store methods it uses, so that a change made for one caller does not churn every other caller and its test double.
31. As a developer, I want a page's chunks removed when the page is removed, so that the corpus cannot contain chunks with no page.
32. As a developer, I want an address longer than the column allows treated as a failed page, so that a truncated address cannot silently become a different page.
33. As a developer, I want timestamps written and read in one timezone, so that two clients cannot see different times for the same event.
34. As an administrator, I want addresses that vanished from the sitemap reported rather than deleted, so that a broken sitemap cannot empty my corpus.
35. As an administrator, I want each logged question to record the last few conversation turns it arrived in, so that an unanswered question can be read in context.
36. As an administrator, I want each answered question to record which model answered and how long it took, so that a change in behaviour can be attributed.
37. As a developer, I want each logged question to carry the trace identifier of the request, so that a log row and a trace can be joined.
38. As a developer, I want the database to record which migrations have been applied, so that a migrator can tell what still needs running.

## Implementation Decisions

**One implementation, several small interfaces declared where they are used.** A single concrete store type carries every method: read the stored state of every address, upsert a candidate, replace a page's chunks, store embeddings, set an address included or excluded, publish, and read and write system state. Nothing else in the system talks to the database.

No package exports a wide interface listing all of them. Each consumer declares the two to four methods it actually calls, next to the code that calls them. The ingest path declares what ingest needs; the review handlers declare what review needs. One test double can satisfy all of them, so this costs nothing in test code and keeps a caller from depending on methods it never uses.

**Transactions never cross the boundary.** No method hands out a transaction handle. Publish is a single method that is internally one transaction, with the retire-before-promote ordering sealed inside it. Exposing the transaction would let every caller get that ordering wrong, which is the failure the original system shipped once.

**Nothing database-shaped crosses the boundary either.** No nullable-column wrappers, no driver values, no result sets. Domain structs with ordinary Go types and UTC times. Where a column is nullable, the struct decides per field whether that is a pointer or a meaningful zero value.

**Failures are sentinel errors, matched by identity rather than by message.** A fetch already running, a halted assistant, a publish attempted during a fetch: each is a named error the HTTP layer maps to a status code without knowing which database is underneath. A duplicate-key violation from the uniqueness rules is wrapped into a domain error rather than surfacing as a driver error number.

**Every method takes a context as its first argument**, which is what makes the query timeout real rather than theoretical once model calls of up to thirty seconds run inside the database.

**Two versions per address, enforced by generated columns.** MySQL has no partial unique index. The substitute, which is what the vendor's own migration guide prescribes, is a generated column holding the address when the page is in one state and NULL otherwise, with an ordinary unique index on it. MySQL unique indexes permit unlimited NULLs, so only rows in that state compete. Two such columns reproduce the original's two partial indexes.

This shape came from a prototype run against the real server and is reproduced because prose describes it worse than the declaration does:

```sql
url_candidate VARCHAR(768) GENERATED ALWAYS AS (IF(state='candidate', url, NULL)) STORED,
url_published VARCHAR(768) GENERATED ALWAYS AS (IF(state='published', url, NULL)) STORED,
UNIQUE KEY documents_one_candidate_per_url (url_candidate),
UNIQUE KEY documents_one_published_per_url (url_published)
```

**The multi-index hazard cannot fire, structurally.** `ON DUPLICATE KEY UPDATE` fires on any unique collision and cannot name an index, and MySQL leaves the outcome undefined when one insert collides on several. That cannot happen here because a page holds exactly one state, so at most one generated column is ever non-NULL. This is asserted by a test that tries to produce a row with both populated, not by this paragraph.

**Publish is one transaction, and the order is load-bearing.** Lock the approved candidate set, retire the published pages for those addresses, promote the candidates, delete the retired rows, stamp the publish time. Promoting before retiring fails with a duplicate-key error, measured. The original system shipped that bug once; the ordering is therefore part of the specification and not an implementation detail.

**Addresses are a bounded string, not text.** MySQL refuses to index a `TEXT` column without a key length, so the address column is `VARCHAR(768)`, the largest utf8mb4 value that fits an index key. An address longer than that is a failed page, not a truncated one.

**Timestamps are microsecond `DATETIME`, written and read as UTC, and no column defaults to the current time.** `TIMESTAMP` converts on read according to the session time zone. `CURRENT_TIMESTAMP` is just as bad in the other direction: on a `DATETIME` column it evaluates in the session time zone and stores that wall clock verbatim, so two connections in different zones write different values for the same instant. Every time is therefore written explicitly by the application in UTC. Measured after the fix: two sessions nine hours apart in configured zone wrote timestamps zero hours apart.

Some values are written by the server instead, and they are not exceptions to the reasoning above but consequences of it. The hazard is a function whose result depends on the session's zone; a function that names UTC does not have it, and the schema uses one. The fetch lock's start time exists only to be compared against the server's clock, so it takes that clock: writing it from the application would put one value on two clocks, and a machine running behind would read its own fresh lock as already abandoned. A migration's record of itself takes it because the application passes that file no parameters and so has no value to supply. Test fixtures take it because the instant is irrelevant to what they assert. Added after implementation; see the decision records of 2026-09-07 and 2026-09-08.

**The include and exclude judgement is held against the address, not the page.** A separate table keyed by address carries it, and an address with no row there is included. This is what makes the judgement sticky rather than a rule the upsert must remember: a newly fetched candidate inherits it because it was never on a page. An earlier draft put an `included` column on the page row, which left stickiness to whatever SQL the upsert happened to use.

**The applied migrations are recorded in the database.** A version table lets a migrator decide what still needs running. Each migration records itself in a form that is safe to repeat, so a database brought up by hand is not left looking empty.

**The query log carries no embedding.** Retrieval passes the question's embedding inline through the vector extension's string constructor, so search performs no write. The parallel array columns of the original become a child table holding one row per retrieved chunk with its score and rank, because MySQL has no array type and because the arrays always relied on a positional correspondence nothing enforced.

**System state is one row, enforced by a check constraint.** It holds the halt flag, the session epoch, the fetch lock and its start time, and the last fetch and last publish times.

**Migrations are numbered and idempotent. They are not transactional, because they cannot be.** MySQL commits implicitly on every schema statement, so a failure half-way through a migration leaves the earlier statements applied. Measured: a `CREATE TABLE` inside an explicit transaction survives a `ROLLBACK`.

Idempotence is therefore the only recovery mechanism, which raises the bar on how each statement is written. Every table is created with its indexes, constraints and generated columns declared inline in one create-if-absent statement, so there is no second statement that could apply without the first. Seed rows are inserted in a form that is safe to repeat. A migration that must alter an existing object inspects the catalogue for what is actually there rather than assuming a name.

**Two lookup indexes beyond the uniqueness rules.** The pages table is indexed by state and by address, because the review screen lists candidates by state and the ingest path looks pages up by address on every fetch. Neither participates in any rule; they exist so the two hot reads do not scan.

**No vector index.** The vector extension defines none, because the server does not yet support extension-defined index types. Every search is a sequential scan. This is recorded rather than worked around.

**The vector column is declared at the embedding width.** The embedding provider returns a fixed width and the extension exposes no way to request narrower, and that width is the extension's declared maximum. The configuration validator requires exactly this width. A narrower vector is as unusable as a wider one, because the column is declared at one size and the embedding function offers no way to ask for fewer elements.

## Testing Decisions

**A good test here asserts a rule, not a statement.** The valuable assertion is "a second candidate for one address is refused", not "the code emits this SQL". Tests that pin SQL text break on every harmless rewrite and pass while the database rejects the query.

**Two tiers, split by what they can actually prove.**

The fast tier runs with no database and no network. It covers what stays in Go: statement construction through a mock driver, so a query's shape and arguments are asserted without a server; the consumer-declared interfaces satisfied by one in-memory double, so the callers above them can be tested; and pure functions such as address canonicalisation.

The live tier requires a running server and is skipped when its connection string is absent. It covers everything the fast tier cannot honestly assert: that the generated columns enforce both uniqueness rules, that superseded rows are unconstrained, that no row populates both generated columns, that an upsert leaves the published page untouched, that publish is atomic and correctly ordered, that promoting before retiring fails, that cascade deletion removes a page's chunks, that the migrations run twice with the same result, that the declared vector column width still agrees with the constant the application validates against, that two sessions in different time zones write the same instant, that an address longer than the column is refused rather than truncated, and that the declared vector width agrees with both the application constant and the configuration file.

The skip is reported rather than silent, so a green run never means half of it did not execute.

**The check script owns nothing beyond the code that exists.** It builds, vets, formats, validates the configuration, and runs both test tiers. It does not check the browser assets, because there are none yet; that step returns when they arrive.

**The live tier must be shown to have run.** `go test` exits zero when a name pattern matches nothing, so a live tier that had been deleted or renamed would report success having executed nothing. The check script therefore fails unless at least one package actually ran a test, which is a different condition from "no package reported an empty run" -- packages with no live tests report one legitimately.

**The real configuration file is checked outside the test suite.** Unit tests use invented fixtures only. The guarantee that the live `siteconfig.toml` still parses is a separate step in the check script, run as a small command, so a test never fails for a reason unrelated to the code it covers.

**Prior art.** The configuration package is the pattern for the fast tier: an invented fixture rather than the real file, one behaviour per test, error assertions that name the offending key. The nine review-workflow cases proved during the spike are the pattern for the live tier and become its first tests.

**The tests that matter most are the ones that try to break the rules.** Four of the live-tier assertions are deliberate violations that must be refused. A change that makes them pass is a change that removed a protection.

## Out of Scope

Fetching, extracting, and chunking pages. This spec provides the tables those write into and nothing that fills them.

Embedding generation and similarity search. The columns exist; the retrieval path is a later spec.

The HTTP surface and the administration pages. The store is reachable from Go; nothing serves it yet.

Tuning the similarity threshold, which needs a fixture set for this site that does not exist.

Any vector index, which the database cannot currently support.

Deleting addresses that vanished from the sitemap. They are reported, never removed.

## Further Notes

Every mechanism above was measured against a running server on 2026-09-07 before this spec was written, across nine cases covering coexistence, both uniqueness rules, superseded rows, the generated columns, upsert isolation, publish atomicity, and the ordering failure. This spec describes something already proven to work, not something believed to work.

The reason for the generated-column substitute, the reason there is no vector index, and the reason the query log has no embedding column are each recorded in the porting notes with what was lost and why. A reader who wants to know what this schema would have looked like on Postgres should read that file rather than inferring it from here.

## Added after this specification

Everything below was built during implementation and is not asked for above. It is recorded here so the specification stays the reference for what the code does, rather than a description of an earlier version of it. Each entry says what was added and what asked for it.

**Thirteen sentinel errors, where this specification names six.** The list is given in full rather than counted, because a count is a number that drifts out of step with the thing it counts and nothing notices.

Named here: a fetch already running, a halted assistant, a publish attempted during a fetch, the two duplicate-key violations, and an address longer than the column. Added during implementation, each with its reason:

- **A deadlock or a lock-wait timeout.** These are the two failures where retrying is correct, and every other failure means the opposite. A caller that cannot tell them apart either retries nothing or retries everything.
- **An abandoned fetch.** Described below; it carries behaviour this specification does not ask for.
- **A release refused because the lock is no longer this run's.** A run that overran the expiry has had its lock taken by another fetch. Clearing it then would release a lock a live run is relying on, so the release is refused. No story asks for this; it follows from the expiry existing at all.
- **The system-state row missing.** The migration seeds one row, so its absence means the database was not brought up properly, and that has a different remedy from any driver failure.
- **An embedding of the wrong width.** The column is declared at one width and the embedding function offers no way to ask for another, so a different width is a fault in the caller rather than a configuration choice.
- **The vector extension absent**, and **a server whose widest vector is narrower than an embedding.** Both are refusals to migrate at all, described next.

**A check that the vector extension is present, run before any migration.** The schema declares a vector column and migrations cannot roll back, so applying without the extension leaves the earlier tables created and reports an unrecognised type. The migration file already said the migrator would check first; nothing in this specification said so.

**Connection pool limits.** Go opens an unlimited number of connections by default and keeps each forever. The model functions hold a server thread for a whole outbound call, so an unbounded pool exhausts the server's connection limit rather than making callers wait, and a connection kept past the server's idle timeout fails on a handle that looks fine.

**Publish is refused after an abandoned fetch, not only during a live one.** Story 15 refuses a publish while a fetch runs and story 14 expires a stale lock, and the two together left a hole neither names. After a crash mid-crawl the lock expires, so the next publish saw no lock and promoted whatever the dead run had written, which is half of one crawl and violates story 5 silently. Expiring the lock and trusting the pages written under it are two different decisions; only the first is asked for here. A lock that is present but expired now refuses a publish and permits a new fetch, so a crashed run still cannot lock the system out. The recovery is a fetch that runs to completion, which replaces those candidates and releases the lock.

**The publish transaction states its isolation level.** This specification fixes the ordering inside publish and leaves the isolation level to the server. Locking the candidate set is what keeps a candidate written mid-publish from being silently left behind, and that only holds where a range lock also blocks an insert into the range it covers. Under the weaker level many servers are configured with, the same statement takes no such lock and the publish reports success having missed the new candidate. The level is now requested rather than inherited, and a live test asserts the transaction actually gets it.

**Two check constraints beyond the one specified.** This specification asks for a check constraint enforcing the single system-state row. The schema also constrains a page's state and a logged question's outcome to their known values, so a typo in a future statement is refused by the database rather than stored as a state nothing matches.

**No separate index on a chunk's page.** The uniqueness rule on a page and an ordinal already indexes the page as its leading column, so a second index on that column alone could never be chosen and would be maintained on every write for nothing. A live test now refuses any non-unique index that is a leftmost prefix of another.

**Some values are written by the server's clock**, described in the timestamp paragraph above.

## Not built, and where that is recorded

The query log has no writer. Nothing in Go writes a row to it, which is consistent with the retrieval path being a later specification, and the stories covering unanswered questions are unserved by this work.

No consumer declares an interface over the store, because no consumer exists yet. The in-memory double the testing section describes is recorded in `DEFERRED.md` rather than written against interfaces nobody has declared.

Addresses that vanish from the sitemap are reported rather than deleted only in the sense that nothing deletes them. No code compares a sitemap to the stored addresses, because nothing fetches a sitemap yet.
