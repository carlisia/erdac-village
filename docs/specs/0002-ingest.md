# 0002 - Ingest

Status: implemented 2026-09-08. Written 2026-09-08, after the site survey and after every decision it depended on was made. Reviewed and approved the same day with three answers, folded in below.

## Problem Statement

The store can hold pages, chunks and vectors, and publish them. Nothing puts anything in it. Until something does, the assistant has nothing to answer from and the review screen has nothing to show.

Filling it is not a download loop. The original system found, one at a time, a set of ways a downloaded website produces a knowledge base that looks healthy and answers confidently while wrong: content assembled in the visitor's browser that a downloader never sees, navigation repeated on every page, sections hidden from readers but present in the markup, pages about other businesses, dates that lie. The survey of this site on 2026-09-08 confirmed four of those, cleared three, disproved one rule this repository had been carrying, and found two nobody had listed. Every one of them is silent. This specification is the list of defences, each one tied to the hazard it exists for.

A second problem is specific to this port. The original embedded text by sending it to a provider in one batch and getting vectors back. Here, embedding runs inside the database, one row at a time, through a function that reports failure by returning nothing rather than by raising. The pipeline has to work with that rather than around it, and it has to notice every silent failure.

## Solution

One run reads the site's sitemap, decides which addresses are worth downloading, downloads them a few at a time, turns each page into plain text with the site's chrome removed, checks that text the site is known to carry actually arrived, splits each page at its headings into chunks sized for the embedding model, has the database embed each chunk, and stores every changed page as a candidate for review. It ends with a report an administrator can read that accounts for every address the sitemap listed.

Nothing a run does reaches a visitor. A run writes candidates; publishing is the separate, all-or-nothing step the previous specification built.

The run is driven from a command line. The administration pages that will also drive it are a later specification, and the command line is shaped so they can share one implementation rather than two that drift.

## User Stories

1. As an administrator, I want a fetch to read the site's sitemap and download every page worth having, so that I do not maintain a list of pages by hand.
2. As an administrator, I want a fetch to skip addresses that exist only to forward a reader elsewhere, so that the review list is not padded with empty pages.
3. As an administrator, I want a fetch to skip addresses the site's own robots file disallows, so that the assistant never answers from a page the site asked crawlers not to take.
4. As an administrator, I want a fetch to skip address groups I have configured as excluded, so that pages about other people never enter the knowledge base.
5. As an administrator, I want a fetch to skip addresses I excluded during review without downloading them, so that my decision costs nothing to keep and stands until I reverse it.
6. As an administrator, I want one page that will not download to be reported rather than to stop the run, so that a broken page does not cost me the rest of the site.
7. As an administrator, I want a fetch to stop and save nothing when text the site is known to carry did not arrive, so that a knowledge base with holes in it is never left for me to publish.
8. As an administrator, I want a fetch to skip pages whose content has not changed, so that a refresh is fast and cheap.
9. As an administrator, I want to force a fetch to process every page, so that a site whose dates are wrong can still be brought up to date.
10. As an administrator, I want a report of what a fetch did that accounts for every address in the sitemap, so that a number that does not add up is a fault I can see rather than a page I have to hunt for.
11. As an administrator, I want to be told which live pages the sitemap no longer lists, and I want nothing done to them, so that a broken sitemap cannot empty the knowledge base.
12. As an administrator, I want a second fetch refused while one is running, so that two runs cannot mix their pages into one review list.
13. As an administrator, I want a slow fetch to keep its place, so that a crawl that takes longer than expected is not mistaken for one that crashed.
14. As an administrator, I want a fetch that has lost its place to stop writing, so that a run that was mistaken for crashed cannot mix its remaining pages into the next run's.
15. As an administrator, I want publishing refused after a crashed fetch until a full fetch has replaced every page, so that a mixture of two runs never goes live.
16. As an administrator, I want the run to record when it finished only if it finished, so that a failed run does not claim the site was checked.
17. As an administrator, I want to see the pages waiting for review with whether each one changed, so that I can decide quickly.
18. As a visitor, I want the text the assistant answers from to be what a reader sees, so that it never quotes navigation, footers, or sections hidden from the page.
19. As a visitor, I want a sentence with a styled word in it kept whole, so that the assistant never answers from a fragment.
20. As a visitor, I want a postal address written on separate lines to stay readable, so that the assistant can find it and read it back.
21. As a developer, I want each page split at the heading levels the site actually uses, so that a page never becomes one enormous unusable chunk.
22. As a developer, I want a short section merged into its neighbour and a long one split with overlap, so that a chunk is never too small to answer anything or too large to sit beside the others.
23. As a developer, I want each chunk to carry the trail of headings it sat under, so that an answer can say where it came from.
24. As a developer, I want no chunk to exceed the embedding provider's hard limit, so that the provider never refuses one outright.
25. As a developer, I want a failed embedding noticed and reported for the page it belongs to, so that a page is never stored with a chunk the assistant cannot find.
26. As a developer, I want the provider key held where statement logs cannot see it, so that a query log is not a key leak.
27. As a developer, I want a sitemap entry that is not an address skipped and named in the report, so that a malformed entry is visible rather than silently absent.
28. As a developer, I want page text extracted with a real parser, so that markup and script source never become body text.
29. As a developer, I want the whole run to work against sample pages with no network and no database, so that every hazard has a test rather than a paragraph.
30. As a developer, I want the sample pages invented and each one to reproduce one hazard, so that a failing test can never be mistaken for a real answer about a real site.
31. As a developer, I want the command line and the administration pages to share one implementation of a fetch, so that the two cannot disagree about what a fetch does.

## Implementation Decisions

**Two seams, declared where they are used.** The run takes two things as arguments: somewhere to read a sitemap and pages from, and somewhere to keep what it produced. Each is an interface of the two to four methods the run calls, declared in the package that runs it. The live implementations are an HTTP client and the store from the previous specification. The test implementations read sample pages from a folder and keep results in memory. This is what lets the whole pipeline run with no network and no database, and it is the in-memory double the previous specification deferred until a consumer existed.

**The frontier is decided before anything is downloaded, in a fixed order, and every address lands in exactly one bucket.** For each sitemap entry: an entry that is not an absolute address is skipped and named in the report by its exact text; an address matching a configured robots pattern is skipped; an address matching a configured excluded pattern or group is skipped; an address with a single path segment is skipped; an address the administrator excluded during review is skipped without being downloaded; everything else is downloaded. After cleaning, a page whose text falls under the configured word floor is dropped as thin, which is what removes a folder listing whose article arrives empty. The report has one list per bucket plus lists for thin, skipped-as-unchanged, stored and failed, and the sum of every list equals the number of entries the sitemap gave. A total that does not add up is a fault the report makes visible.

**Why a single path segment.** The survey measured that every alias stub on this site has exactly one path segment and every real page has more, that the capitalisation rule the configuration carried caught about a quarter of the stubs sampled, and that every stub's real target is listed in the sitemap separately. A stub does not redirect: it answers success with a refresh instruction in the head. So the shape is the only rule that works before downloading, and it costs nothing on this site. A re-run of the survey is the check that it still holds.

**Why a malformed entry is skipped and named rather than repaired or fatal.** Measured: an entry that is bare text containing a colon parses without error, the text before the colon becomes a scheme, and resolving it returns the text unchanged. Repairing it by guessing fetches a page that may not be the one meant and says nothing about having guessed. Refusing the whole run for a typo in a file the site tool generates holds every other page hostage to it.

**Robots patterns come from configuration.** The configured list mirrors what the site's robots file disallows. Fetching and parsing that file live, and honouring a crawl delay, are recorded in the deferred work and are not built here.

**Cleaning follows the original's rules exactly, with a real parser.** A line break becomes a space before parsing, so an address written on lines stays readable. Script, style, template, and the head are removed before any word is read. The configured chrome selectors are removed. Headings become markdown headings at the level the tag says. Of the remaining elements, text is taken from the outermost element that contains no block-level child, so that a sentence with a styled word in it is read once and whole, and a container and its parent are never both read. List items are prefixed. The result is joined with blank lines, and its hash is the page's fingerprint. This is the rule the original arrived at through several wrong versions, each recorded in its tests, and every one of those tests is carried over against invented pages.

**The parser is a real HTML parser, never a pattern.** The survey found that a pattern-based stripper leaks attribute contents, including script source, into the text, because a `>` inside an attribute value ends the match early. The Go HTML library the plan already names is used and nothing else.

**The canary check reads every downloaded page, including the ones about to be skipped, and runs before anything is written.** The text an administrator names as required often lives on a page that has not changed. Checking only the changed pages would stop a run that had nothing wrong with it. And nothing may be written before the check, because a knowledge base with holes in it looks exactly like a working one. The configuration names no required text, by decision of 2026-09-08: the survey found article text arrives in the served page, and no phrase was chosen. The check is therefore off until one is configured, and the command line accepts one for a single run.

**A page is unchanged only when both its date and its fingerprint match, and a missing date never matches.** The dates come from the site's own tool and are not always present or right. The comparison is against the page's candidate if one is waiting, otherwise against the published page, so a second download during a review does not re-process text it already holds. Force bypasses the comparison and nothing else.

**Sitemap dates are parsed to a time, and a date that cannot be parsed is no date.** The original compared dates as text after writing both the same way. Here the store holds a time, so the sitemap's text is parsed on the way in. A text the parser cannot read is treated as absent, which means the page is downloaded again rather than skipped.

**Splitting happens at the configured heading levels, keeps the heading trail, merges short sections forward, and splits long ones with overlap.** A section below the floor is carried into the next; a trailing carry joins the last section. A section above the ceiling is cut into chunks that overlap at the seam so a sentence is not cut in half. Ordinals run from zero in page order.

**Tokens are counted by approximation, and one bound is hard.** The original counted with the tokenizer its embedding model used. This model's tokenizer has no Go implementation, so a chunk's tokens are its word count multiplied by a configured factor, biased to over-count. The floor and ceiling are soft targets. The provider's input limit is not: a chunk over it is refused by the provider outright, so a chunk whose estimate exceeds the configured cap is a fault of this code and the run reports it as one rather than sending it.

**Embedding runs inside the database, one chunk at a time, on one pinned connection, with the key in a session variable.** The previous specification recorded why: the function that embeds is a database function, it holds a server thread for the whole call, a session variable belongs to one connection, and a key inline in a statement is visible in every log. The run takes one dedicated connection for the whole embedding phase, sets the key in a session variable on it, sets the statement timeout on it, and calls the function once per chunk. Each result is wrapped in a character-set conversion, because the function returns a binary string and the vector column refuses it otherwise.

**A failed embedding fails the page, and nothing about that page is written.** The embedding function reports failure by returning nothing and raising a warning. Every result is checked. A page with any chunk that came back empty is reported as failed with the warning's text, and none of its chunks are stored, so a page is never searchable through half of itself. The original stopped the whole run when the count of vectors did not match the count of texts, because it paired them by position; here each chunk is embedded on its own and there is no position to get wrong, so the failure is confined to the page. A provider outage fails every page and the report says so.

**Vectors arrive as text and go back as text.** The embedding function returns the vector's text form. The store takes a Go slice, so the text is parsed on the way in, which also checks the width, and the store writes it back as a literal. This round trip is recorded in the porting notes and is not a choice this specification makes.

**Storing a page is one store call, in one transaction.** The candidate, its chunks and their vectors are written together or not at all. The original made three separate calls and accepted the window between them, in which a process dying left one page with chunks that had no vectors. This port closes it: one method takes the run's identifier, the page, its chunks and their vectors, checks the run still holds the lock, and writes all three inside one transaction. The three separate methods the previous specification described are replaced by it, because nothing else would call them and a method nothing calls is a second way to get the ordering wrong. The previous specification's record of its own additions is updated to say so.

**The lock is renewed, enforced, and its takeover is recorded.** This is the three-part fix the decision of 2026-09-08 settled, and all three parts land here. First, a running fetch renews its lock at an interval well inside the expiry, so a slow crawl is not mistaken for a dead one. Second, the one store method that writes a candidate takes the run's identifier and refuses a run that no longer holds the lock, so a run that lost its place cannot write another page. Third, taking over an abandoned lock records that a takeover happened, publishing is refused while that record stands, and only a fetch run in force mode that completes clears it, because only a force run has demonstrably replaced every page. A new migration adds the column that holds the record, which is also the first time the migrator applies more than one file.

**Vanished pages are reported, never removed.** After the run, every published address the sitemap did not list is named in the report. A sitemap that came back broken, truncated or empty would otherwise silently empty the knowledge base.

**The finish time is written only by a run that finished, and only a force run in which no page failed may say it replaced every page.** The store already releases the lock with or without a fetch time; a run that failed releases without one. The claim that every page was replaced is what clears a recorded takeover, and a force run with one failed page has left that address holding whatever candidate it had before, which may be a crashed run's. Found by review after the first build.

**One implementation of a fetch, driven two ways.** The command line takes the lock, runs, releases in a deferred call whatever happened, and prints the report. The administration pages, when they exist, will take the lock first so they can answer at once when a fetch is already running, and call the same run without taking the lock again. The run therefore neither takes nor releases the lock; the caller does. The original's tests pin exactly this and they are carried over.

**The command line has three subcommands.** `fetch`, with `--force`, `--fixtures` to run against the sample pages with nothing saved, and `--require` to name canary text for one run. `review`, which lists every candidate with whether its text differs from the published version. `publish`, which calls the store's publish and prints what it did. Exit codes are distinct for no key, lock held, and canary missing, so a script can tell them apart.

**The provider key comes from one environment variable.** Named for this system and the provider. A missing key is an error before any connection is made.

## Testing Decisions

**The fast tier runs the whole pipeline against invented pages, offline.** The sample source reads a folder; the sample store keeps results in memory; the sample embedder returns one vector per chunk. Every test asserts something an administrator could see in the report or the review list, never how the pipeline is arranged inside.

**Every hazard has a sample page, and every sample page is invented.** The original's pages are carried over: a normal page with navigation and a footer, a page with a hidden section, a page about another business, a page carrying a single price, a page whose content is assembled in the browser, a page with text in plain containers, a page with a styled word mid-sentence, a page with a stylesheet inside a container, a page with a postal address on separate lines. This site's survey adds: a stub that answers success with a refresh instruction, a folder listing whose article is empty, and a page whose markup carries script source inside an attribute. The sample sitemap adds two malformed entries. None of the content is real, and the test configuration names invented address groups.

**The cleaning tests are the original's, one per hazard.** Each asserts what a reader would see: navigation gone, hidden section gone, headings kept, a sentence kept whole, a fragment never standing alone, stylesheet text never emitted, a line break separating words.

**The chunking tests are the original's, adjusted for word-count tokens.** Splits at configured levels and not deeper; heading trails; short sections merge; long sections split under the ceiling with overlap; no content lost; ordinals from zero; a chunk over the provider cap is refused.

**The pipeline tests are the original's plus this port's.** Saves what the sitemap lists; a saved page is a candidate; excluded groups never reach the store; hidden text never reaches the store; an unchanged page is not processed again; force processes it; a missing canary stops the run and nothing is saved; the canary check reads skipped pages; an administrator's exclusion is not downloaded; one failed page does not stop the rest; every chunk gets a vector; the report lists every address; a sample page with no stand-in is reported. Added: a single-segment address is never downloaded; a malformed entry is named; a robots-disallowed address is never downloaded; a chunk that fails to embed fails its page and nothing of that page is stored; the report's buckets sum to the sitemap's count.

**The live tier covers what only the database can prove.** The embedding function is called through the store's pinned connection with the key in a session variable and returns a vector of the declared width; a NULL result is noticed; the writer refuses a run that does not hold the lock; renewal moves the start time; a takeover is recorded and refuses publish; a completed force run clears it; the second migration applies after the first and is recorded. The live tests that call the embedding provider are skipped without the key as well as without the database, and the skip is reported.

**New tests are broken before they are trusted.** Each new behaviour is verified by breaking the thing it tests and confirming the test fails, and the mutation changes the system under test, never the test.

**The check script gains one step.** The sample-pages run, `fetch --fixtures`, executes as part of the fast tier, so the whole pipeline is exercised end to end on every check with nothing saved.

## Out of Scope

Answering. Retrieval, the similarity cutoff, prompt assembly and the model call are the next specification. This one fills what they read.

The administration pages. The run is shaped for them to call; nothing serves it over HTTP yet.

Fetching and parsing the site's robots file, and honouring a crawl delay. Recorded in the deferred work. The configured pattern list stands in.

Telemetry. The original measures a fetch and its embedding phase as spans. The tracing package is empty and the plan records the choice of library; wiring it is its own piece of work.

Tuning the chunk sizes against the provider's real tokenizer. The word-count factor is inherited with its derivation and re-tuning is expected to confirm it.

Discarding an interrupted crawl's pages on request. The decision keeps them; the administration pages will offer the discard.

## Further Notes

The original's ingest module, its tests and its sample pages were read in full before this was written. Behaviour is mirrored; structure is not. Where this specification departs from the original, the departure is one of four kinds, each recorded in the porting notes: the database embeds instead of a provider call; tokens are approximated instead of counted; the lock is enforced instead of advisory; and three frontier rules exist that the original's site never needed.

Every store change this specification requires is listed here so the previous specification's record stays accurate: a page summary carries its last-modified date; one method stores a candidate with its chunks and vectors in one transaction, takes the run identifier, and replaces the three separate writers; a renewal method exists; a takeover is recorded in a new column, refuses publish through a new sentinel, and is cleared by a completed force run; an embedding method runs the database function on a pinned connection. The additions section of the previous specification is updated when they land.

## Added during implementation

Recorded so this specification describes the system rather than an earlier version of it.

**A cap on the size of a downloaded page.** The original had none. A page larger than sixteen million bytes is not a page, and reading it to the end would hold memory for nothing, so the download stops there and the page is reported as failed.

**A bucket for a duplicated sitemap entry.** A sitemap can list one address twice. The first listing is kept and the rest are counted in their own bucket, so the report's total still equals the number of entries the file gave and a duplicate does not make the accounting announce a fault the run did not commit.

**The completion rule.** A run tells the store it replaced every page only when it ran in force mode and no page failed. Described above under the finish time.
