# Exceptions

Shortcuts taken because this is a demonstration. Each entry says what was done, why it is acceptable here, and what production would require instead. None of these are bugs; all of them are choices that would be wrong in a real deployment.

## The database is alpha software

**What we did.** The system is built on VillageSQL, which its own README describes as alpha, intended for development and testing, and not recommended for production. Two of the extensions it depends on carry their own instability warnings, and one builds against an explicitly unstable interface.

**Why acceptable here.** Meeting the rough edges is the purpose of this repository, not a side effect of it.

**What production requires instead.** A stable release, or a different database.

## API keys are passed as function arguments

**What we did.** Both provider keys reach the database as arguments to a SQL function. They are held in session variables so they do not appear in statement text.

**Why acceptable here.** The extension offers no other mechanism. Its own documentation lists environment-variable support as a future enhancement and warns that keys passed as parameters may be visible in query logs, slow query logs, and process lists.

**What production requires instead.** Keys held outside the database, or an extension that reads them from the environment or a keyring.

## Failures are inferred from NULL

**What we did.** Model and embedding failures return SQL NULL and a warning rather than raising, so an error is detected by checking for NULL rather than by catching anything.

**Why acceptable here.** It is the extension's documented behaviour and cannot be changed from outside it.

**What production requires instead.** Errors that abort the statement, so a half-failed ingest cannot report success while writing NULLs into an embeddings column.

## Every similarity search is a full scan

**What we did.** No vector index exists, because the server does not yet support extension-defined index types.

**Why acceptable here.** One site of this size produces a corpus small enough that scanning all of it per question is not noticeable, and performance is explicitly out of scope.

**What production requires instead.** An index. At corpus sizes beyond a few thousand chunks a sequential scan per question stops being viable.

## The fetch lock is advisory

**What we did.** A marker in the database stops a second fetch starting while one is running, and stops a publish promoting a half-written set of pages. Nothing in the database enforces it. The three operations that write pages do not check it, so the guarantee holds only because the code that fetches takes the marker first.

**Why acceptable here.** One person runs one fetch at a time, through one path, and that path takes the marker. The documentation on the marker says plainly that it is advisory and that anything writing pages must take it.

**What production requires instead.** The check moved into the writes themselves, so that a page written without the marker is refused by the database rather than by a convention, or a lease the database validates on every write.

## Single tenant, one site, one admin

**What we did.** One configuration file describes one website, and one shared password gates the admin portal.

**Why acceptable here.** It is a demonstration of a product shape, not a service.

**What production requires instead.** Per-tenant configuration and isolation, individual accounts, and an authentication mechanism that is not a shared secret.
