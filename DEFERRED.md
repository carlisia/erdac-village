# Deferred

Work that is postponed rather than rejected. Each entry says what it is, why it matters, and what would unblock it. An entry missing any of the three is useless and should be completed or removed.

## Redacting personal information before it reaches monitoring

**What.** Question and answer text is sent to the tracing backend truncated but otherwise unmodified. A single function is the only path by which a person's typed words leave this system, so there is one place to add redaction, and it is not written yet.

**Why it matters.** A visitor can type anything into a chat box, including their own name, email address or phone number. Anything they type is currently readable by anyone with access to the tracing workspace.

**What unblocks it.** A decision on what to redact and how aggressively, then an implementation in that one function, then a test that asserts a known pattern does not survive it. Carried over from Erdac, where the same gap exists.

## Honouring robots.txt during the crawl

**What.** The crawler reads the sitemap and downloads what it finds. It does not fetch or obey `robots.txt`, and it applies no crawl delay or rate limiting.

**Why it matters.** The target site's `robots.txt` disallows a path group that its own sitemap lists, so sitemap membership and permission disagree today, and following the sitemap alone fetches pages the site asked crawlers not to take. The survey of 2026-09-08 confirmed the overlap is about a fifth of the sitemap. That file also states usage terms in comments: it asks for attribution by name and forbids reproducing more than a hundred words verbatim. Comments are not directives, so no parser will enforce either one.

**What unblocks it.** Wiring the chosen robots parser into the frontier filter, and deciding whether the stated terms are honoured as policy. Carried over from Erdac.

## Reranking, or a per-topic rule, for the retrieval cutoff

**What.** A single similarity threshold decides whether the model is called. On the original site the questions that must be answered and the questions that must be refused overlapped in score, so no single value separated them and some questions were always going to be wrong.

**Why it matters.** Every question below the cutoff is a visitor told nothing at all, and every irrelevant question above it is a wasted model call.

**What unblocks it.** Either a reranking pass over the retrieved chunks before the gate, or per-topic thresholds. Both need the new fixture set to exist first, since there is nothing to evaluate against until then.

## Building the VillageSQL server from source

**What.** The prebuilt dev-server tarball is used. The server itself is not compiled locally.

**Why it matters.** Building the server means owning its protocol version, which is what the dev-ABI extension must match exactly. It is also the configuration in which a contribution to the server would be developed and tested.

**What unblocks it.** The spike holding against the prebuilt server first, so that a rejected extension can be attributed to the change rather than to two unknowns at once.

## Evaluating `gemini-embedding-2`

**What.** `gemini-embedding-001` is the chosen embedding model. Its successor accepts four times the input.

**Why it matters.** The 2048-token input limit of `-001` is a hard provider cap that an approximate token count could exceed, and the chunk ceiling has to be defended against it. An 8192-token limit would remove that hazard.

**What unblocks it.** Confirming that `ai_embedding` accepts the newer model identifier, and deciding whether a newer multimodal model is a fair substitute for a text-only one in a text-only pipeline.

## Checking the browser assets

**What.** The check script had a step that ran a syntax check over every JavaScript file in the public directory. It was removed, because that directory is empty: the browser assets are copied across in a later phase.

**Why it matters.** Those assets arrive as a verbatim copy from the original system, with no build step, so a syntax error in them fails silently in a browser rather than loudly in a check run. The step is the only thing that would catch one.

**What unblocks it.** Copying the browser assets across. Restore the step in the same run.

## A derived host name for the browser pages

**What.** A helper that stripped the scheme and trailing slash from the configured site address was written and then removed, because nothing called it.

**Why it matters.** The original system's pages render the site's host name, and the endpoint that feeds them derives it rather than storing it twice, so that the two cannot drift apart.

**What unblocks it.** The endpoint that serves site details to the browser. Derive it there rather than adding a second configuration key.

## Measuring the fetch lock expiry

**What.** A fetch lock older than thirty minutes is treated as abandoned, so a crashed run does not lock the system out permanently. Thirty minutes is a judgement, not a measurement.

**Why it matters.** The number is wrong in one direction or the other and only one direction is dangerous. Too long, and a crashed run blocks fetching for longer than it should, which is visible and recoverable. Too short, and a healthy fetch that overruns has its lock taken by a second fetch, and the two interleave into one candidate set that looks healthy and mixes two crawls.

**What unblocks it.** One full crawl of the site, timed. The expiry should then be set well above the longest run observed, not close to it.

## A stand-in database for the code that will use the store

**What.** The store is the single piece of code that talks to the database. Anything else that needs to read or write data will go through it. The tests are split in two: a fast set that runs anywhere with no database at all, and a slow set that needs a real server running. For the fast set to cover the code that uses the store, that code needs something to talk to instead of the store: a stand-in that keeps its data in memory and behaves the same way. It is not written yet, because nothing uses the store yet. The parts that will -- the code that fetches pages, and the pages an administrator reviews them on -- are specified separately and come later.

**Why it matters.** Without the stand-in, none of that code can be tested without a running database, so it either goes untested or its tests only run on a machine that has one set up by hand. Keep the fast set runnable with no setup at all, because a test that needs a machine prepared by hand is one that gets skipped.

**What unblocks it.** The first piece of code that uses the store. Write the stand-in beside it, and make it cover what every later user of the store needs rather than only the first one.

## One shared setup for the tests that need a real database

**What.** Two groups of tests need a real database server. Each one creates a temporary database, builds the tables inside it, runs, and deletes the database afterwards. That setup is written out twice, once in each group, because the two build the tables by different routes: one runs the file of table definitions directly, the other runs the program that applies them.

**Why it matters.** Two copies of the same setup drift apart. A correction made to one -- how the temporary database is named, say, or how the server address is assembled -- silently leaves the other on the old behaviour, and the group that was not corrected keeps passing while testing something slightly different.

**What unblocks it.** A third group of tests needing the same setup, or any correction that has to be made in both places. Either is the point at which sharing one copy costs less than keeping two.

## Turning three clusters of near-identical tests into tables

**What.** Three groups of tests in the store's fast tier have the same shape and differ only in their inputs: seven covering publishing, four covering how a database error becomes a domain error, and four covering the fetch lock. Each group is written as separate functions rather than as one function looping over a list of cases.

**Why it matters.** Adding a case means copying a whole function. A copy that leaves out the final check, the one asserting that every expected database call actually happened, still passes while testing nothing. That is a test reporting success while exercising nothing, which is the failure the whole suite exists to prevent.

**What unblocks it.** Nothing external; this is deliberate sequencing. Every one of these tests has been verified by breaking the behaviour it covers and confirming it fails. Rewriting them discards that verification, so each rewritten test has to be broken and re-confirmed one at a time. Do it as its own piece of work, not folded into another change, and re-run the mutation checks afterwards. Start with the error-translation group, whose four cases already differ only in one input and one expected result.

## Confirming the number that says the vector extension is absent

**What.** Before applying the schema, the store asks the server for its widest vector. A server without the extension does not know that function and refuses with a specific numbered error, and only that number is treated as meaning the extension is absent. That the server uses that particular number for an unknown function is taken from the database engine's documented behaviour, not from observation.

**Why it matters.** If the real number is different, the check stops recognising the case and the error passes through unchanged. That is the safe direction to be wrong in, because nothing is misreported, but the helpful message never appears and the operator sees a raw failure instead.

**What unblocks it.** A server with the extension uninstalled, and one call to that function against it. Record the number observed and promote the note in the code from unmeasured to measured.

## Discarding the candidate set left by a fetch that crashed

**What.** When a fetch stops without finishing, the marker it took stays in the database. Publishing is then refused, because the pages that run wrote are half of one pass over the site and promoting them would put a mixture of two passes in front of visitors. The only way out today is to run another fetch all the way through, which replaces those pages and clears the marker.

**Why it matters.** An administrator who does not want to re-fetch has no other option. There is no way to say "throw away what the failed run wrote and carry on with what is already live". Publishing stays refused until a fetch succeeds, which is safe but is not always what the person wants.

**What unblocks it.** The administration pages, which are specified separately. Add one action there that deletes the pending pages and clears the marker in a single step, and make it say how many pages it is discarding before it does so.

## Making the fetch marker actually prevent two overlapping crawls

**What.** A marker in the database is meant to stop two crawls running at once and mixing their pages into one set awaiting review. It does not currently do that, in three ways that have to be fixed together.

The marker is treated as abandoned after a fixed age and nothing refreshes it, so a crawl that takes longer than that age has its marker taken by the next crawl to start. Nothing stops the first crawl carrying on: the three operations that write pages never look at the marker. And once a second crawl takes the marker over and finishes normally, nothing records that this happened, so publishing sees a healthy state and puts a mixture of two crawls in front of visitors.

**Why it matters.** The result is a knowledge base assembled from two passes over the site, taken at different moments, with no report that anything went wrong. It is the exact failure the marker exists to prevent, and the only symptom is answers drawn from pages that were never live together.

**What unblocks it.** A decision that cannot be made yet: what should happen to the pages the interrupted crawl already wrote. Discarding them is the only thing that guarantees a clean set, and it destroys pages an administrator may be part-way through reviewing. Keeping them means publishing has to stay refused until a crawl has demonstrably replaced every one, and whether a crawl has done that depends on which pages it chose to skip as unchanged.

That skip rule is defined by the work on fetching, which is specified separately. Settle it there, then fix all three parts in one change: refresh the marker while a crawl runs, refuse a page write from a crawl that no longer holds it, and record a takeover so publishing refuses until the set is known to be clean.
