# Deferred

Work that is postponed rather than rejected. Each entry says what it is, why it matters, and what would unblock it. An entry missing any of the three is useless and should be completed or removed.

## Redacting personal information before it reaches monitoring

**What.** Question and answer text is sent to the tracing backend truncated but otherwise unmodified. A single function is the only path by which a person's typed words leave this system, so there is one place to add redaction, and it is not written yet.

**Why it matters.** A visitor can type anything into a chat box, including their own name, email address or phone number. Anything they type is currently readable by anyone with access to the tracing workspace.

**What unblocks it.** A decision on what to redact and how aggressively, then an implementation in that one function, then a test that asserts a known pattern does not survive it. Carried over from Erdac, where the same gap exists.

## Honouring robots.txt during the crawl

**What.** The crawler reads the sitemap and downloads what it finds. It does not fetch or obey `robots.txt`, and it applies no crawl delay or rate limiting.

**Why it matters.** The target site's `robots.txt` disallows a path group that its own sitemap lists, so sitemap membership and permission disagree today, and following the sitemap alone fetches pages the site asked crawlers not to take. The site also states terms in comments in that file.

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
