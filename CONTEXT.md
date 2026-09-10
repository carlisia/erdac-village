# Glossary

Terms used across this repository where two readers could reasonably disagree on the meaning. Definitions only. No decisions and no design; those live in `DECISIONS.md` and `PLAN.md`.

## The knowledge base

**Address.** A URL on the crawled site. One address can hold more than one row in the database at the same time, which is why "page" is avoided as a synonym.

**Page.** One row in the `documents` table: the cleaned text of one address at one point in time, in one state. Two rows for the same address are two pages.

**Candidate.** A page that has been fetched but not approved. Not searchable. An address may hold at most one candidate.

**Published.** A page that an admin approved. Searchable. An address may hold at most one published page.

**Superseded.** A page that was published and has just been replaced during a publish. A transient state that exists only inside the publish transaction; no superseded row survives it.

**Included / excluded.** An admin's standing judgement about an address. Excluded addresses are never downloaded again and never searched, even if they are already published. The setting is sticky: it survives refreshes.

**Address setting.** An administrator's standing judgement about an address, held separately from any page. This is what makes exclusion sticky: a newly fetched candidate inherits it because it was never stored on a page in the first place. An address with no setting is included.

**Recent turns.** The last few messages of the conversation a question arrived in, stored on the log row so an administrator reading an unanswered question can see what was being discussed. Not the whole conversation, which this system never stores.

**Chunk.** A slice of one page's text, sized for embedding. Chunks carry the heading trail they were found under. "Chunk" is the word in code and in every technical document. The admin portal alone calls it a "piece", because "chunk" means nothing to a non-technical reader.

**Corpus.** Every chunk of every published, included page. What a question is searched against.

## Working with the site

**Frontier.** The set of addresses a fetch will actually download, decided before anything is downloaded. Every sitemap entry lands in exactly one bucket.

**Bucket.** One of the lists in a fetch's report, each holding the addresses that met one fate: not an address, listed twice, disallowed by the robots list, excluded by a configured pattern, a stub by shape, excluded by the administrator, thin, unchanged, stored, or failed. The buckets add up to the number of entries the sitemap gave, so a page that vanished from the count is visible.

**Stub.** An address that exists only to send a reader to another page. On this site a stub answers success with an instruction to go elsewhere rather than redirecting, and it has exactly one path segment where every real page has more.

**Chrome.** The parts of a page that repeat on every page: navigation, header, footer, sidebar, and on this site a backlinks block and a graph view. Removed before a page's text is kept, because left in they land in every chunk and make unrelated pages score high for whatever they advertise.

**Thin.** A downloaded page whose text, after the site's chrome is removed, falls under the configured word floor. Dropped and reported. A folder listing whose article arrives empty is the usual case.

**Refresh.** A fetch that skips addresses whose content has not changed since the last time they were stored. The default.

**Force.** A fetch that processes every address regardless of whether it changed.

**Publish.** The single transaction that promotes every approved candidate to published, retires the pages they replace, and deletes the retired rows. Ordering matters inside it: retire before promote, because the uniqueness rule forbids two live pages for one address.

**Canary.** A string that must appear somewhere in the downloaded text. A missing canary aborts the run before anything is written, and it is the only defence against a site that assembles its content in the browser, where a downloader gets well-formed empty containers.

**Fetch lock.** A marker held in the system state for the duration of a fetch, so a second fetch cannot start and interleave its pages into the first one's candidate set. It carries the time it was taken, and a lock older than a set age is treated as abandoned rather than held, so a crashed run does not block the system permanently.

## Answering

**Extract.** One retrieved chunk as it appears in the prompt, with its heading trail and source address. The model is told to answer only from the extracts it is given.

**Cutoff gate.** The similarity threshold check that decides whether the model is called at all. Below it, the question is declined without any model call.

**Declined.** The visitor was told the pages do not cover their question. Four different situations produce it: nothing was retrieved, nothing scored above the cutoff, the model was called and said the extracts did not answer, or an error occurred. The reason code distinguishes them.

**Sentinel.** The exact string the model is instructed to reply with when the extracts do not answer the question. Unrelated to a sentinel error, defined below. Only the opening of the reply counts, so a refusal followed by an apology still refuses, and an answer that merely quotes the string later still answers. The sentinel text never reaches the visitor.

**Trace identifier.** A handle written on a log row that also appears in the monitoring system, so one question can be followed from the administrator's table into the trace of what actually happened. It exists whether or not monitoring is switched on, so the row always has a handle.

**Unanswered question.** A declined question as it appears in the admin portal. One row per asking, with no de-duplication, because a question asked five times is five visitors who were told nothing.

## Running the system

**Halt.** A server-side flag that makes the chat refuse every question. Checked on every request; the disabled input in the browser is presentation only.

**Epoch.** A counter that open browser tabs compare against. Any change to it, in either direction, makes a tab discard its conversation. It moves in either direction because emptying the database restarts the counter at zero, and a tab testing only for an increase would go permanently deaf.

## The port

**Sentinel error.** A named failure that code returns instead of a message, so that a caller can test which failure happened rather than reading the words. Each one stands for a situation with its own remedy: a fetch already running, an address too long, a database refusing a caller to break a deadlock. Unrelated to the sentinel the model replies with, defined above; the two share a word because both are a fixed value standing for a known case.

**Seam.** A point where a test can substitute the outside world. This system has three: where pages come from, where results go, and what turns a chunk's text into a vector. The third is a database call in production and a stand-in in tests.

Each has one concrete implementation and no wide interface. Consumers declare the two to four methods they call, next to the code that calls them, and one test double satisfies all of those declarations. "The Store seam" therefore names a boundary, not a single type.

**Spike.** A throwaway path through the whole system built before anything else, whose only purpose is to find out which unverified assumptions are wrong.

**Pinned gap.** A feature the stack cannot support, recorded rather than worked around. Recorded twice: in `PORTING.md` for what it cost here, and in `CONTRIBUTIONS.md` for what would fix it upstream.

**Forced / chosen.** The label on every `PORTING.md` entry. Forced means the stack left no option. Chosen means it did, and the entry gives the reason.

**Measured / unmeasured (porting).** The confidence label on every `PORTING.md` entry. Measured means it was observed against a running server, and the label carries the date. Unmeasured means it was not, so it rests on reading rather than on observation.

**Researched / measured (contributions).** The confidence label on every `CONTRIBUTIONS.md` entry. Researched means someone read the documentation and source of the project the entry is aimed at. Measured means it was also observed against a running server. Nothing is filed upstream while still researched.

## VillageSQL

**Extension.** A compiled library the database loads at runtime, registering new types, functions or behaviour. Loaded with `INSTALL EXTENSION`, not MySQL's `INSTALL PLUGIN`.

**VEB.** A VillageSQL Extension Bundle: the distributable file for an extension, containing a compiled library and a manifest. Platform-specific, despite always naming its library with a `.so` suffix.

**Stable and dev ABI.** Two versions of the interface an extension compiles against. Stable is forward compatible: the server must offer at least what the extension asks for. Dev requires an exact protocol match and is rejected on any mismatch.
