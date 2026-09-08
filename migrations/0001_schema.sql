-- 0001 - the whole schema.
--
-- Idempotent, and not transactional. MySQL commits implicitly on every schema
-- statement, so a failure part-way through leaves the earlier statements
-- applied and rollback does nothing. Re-running is the recovery path, which is
-- why every table is declared in one create-if-absent statement carrying its
-- indexes, constraints and generated columns inline. There is no second
-- statement that could apply without its dependencies.
--
-- Requires vsql_vector to be installed. The migrator checks that first and
-- refuses rather than failing here with an unhelpful type error.
--
-- No column defaults to CURRENT_TIMESTAMP. For a DATETIME column that function
-- evaluates in the session time zone and stores the wall clock verbatim, so
-- two connections with different time zones would write different values for
-- the same instant. Every time is written explicitly by the application in
-- UTC, which is the only way one instant reads the same to every client.

-- Which migrations have been applied. The migrator reads this to decide what
-- to run; this file records itself so the table is never empty on a database
-- that has been brought up by hand.
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INT          NOT NULL,
  name       VARCHAR(128) NOT NULL,
  applied_at DATETIME(6)  NOT NULL,

  PRIMARY KEY (version)
) ENGINE = InnoDB;

-- An administrator's standing judgement about an address, held against the
-- address rather than against any page.
--
-- This is what makes exclusion sticky. A newly fetched candidate inherits the
-- judgement because the judgement was never on the page: there is nothing for
-- an upsert to forget to carry forward. An address with no row here is
-- included, so the common case costs nothing.
CREATE TABLE IF NOT EXISTS address_settings (
  url        VARCHAR(768) NOT NULL,
  included   BOOLEAN      NOT NULL DEFAULT TRUE,
  updated_at DATETIME(6)  NOT NULL,

  PRIMARY KEY (url)
) ENGINE = InnoDB;

-- At most one candidate and at most one published page per address, and those
-- two may exist at the same time, which is what lets the assistant keep
-- answering while an administrator reviews a change. Superseded rows are not
-- constrained at all: several may exist for one address inside a publish.
--
-- MySQL has no partial unique index. The substitute is a generated column
-- holding the address only in one state and NULL otherwise, with an ordinary
-- unique index on it: MySQL permits unlimited NULLs in a unique index, so only
-- rows in that state compete. Two such columns reproduce two partial indexes.
--
-- A page holds exactly one state, so at most one generated column is ever
-- non-NULL. That is why no single insert can collide on both indexes, which
-- matters because ON DUPLICATE KEY UPDATE cannot name an index and MySQL
-- leaves the outcome undefined when one insert collides on several.
--
-- The address is VARCHAR(768) rather than TEXT because MySQL refuses to index
-- TEXT without a key length, and 768 is the largest utf8mb4 value that fits an
-- index key. A longer address is a failed page, never a truncated one.
--
-- There is deliberately no `included` column here. See address_settings.
CREATE TABLE IF NOT EXISTS documents (
  id            BIGINT       NOT NULL AUTO_INCREMENT,
  url           VARCHAR(768) NOT NULL,
  title         TEXT         NULL,
  markdown      LONGTEXT     NOT NULL,
  content_hash  CHAR(64)     NOT NULL,
  lastmod       DATETIME(6)  NULL,
  state         VARCHAR(16)  NOT NULL DEFAULT 'candidate',
  fetched_at    DATETIME(6)  NOT NULL,
  published_at  DATETIME(6)  NULL,

  url_candidate VARCHAR(768) GENERATED ALWAYS AS (IF(state = 'candidate', url, NULL)) STORED,
  url_published VARCHAR(768) GENERATED ALWAYS AS (IF(state = 'published', url, NULL)) STORED,

  PRIMARY KEY (id),
  UNIQUE KEY documents_one_candidate_per_url (url_candidate),
  UNIQUE KEY documents_one_published_per_url (url_published),
  KEY documents_state_idx (state),
  KEY documents_url_idx (url),
  CONSTRAINT documents_state_known CHECK (state IN ('candidate', 'published', 'superseded'))
) ENGINE = InnoDB;

-- A slice of one page's text, sized for embedding. Deleting the page deletes
-- its chunks, so the corpus cannot contain a chunk whose page is gone.
CREATE TABLE IF NOT EXISTS chunks (
  id           BIGINT   NOT NULL AUTO_INCREMENT,
  document_id  BIGINT   NOT NULL,
  ordinal      INT      NOT NULL,
  heading_path TEXT     NULL,
  text         LONGTEXT NOT NULL,
  token_count  INT      NOT NULL,

  PRIMARY KEY (id),
  UNIQUE KEY chunks_one_per_ordinal (document_id, ordinal),
  KEY chunks_document_idx (document_id),
  CONSTRAINT chunks_document_fk FOREIGN KEY (document_id)
    REFERENCES documents (id) ON DELETE CASCADE
) ENGINE = InnoDB;

-- One vector per chunk.
--
-- The width is the embedding model's output width, which is also the vector
-- type's declared maximum, and the embedding function exposes no way to ask
-- for a narrower vector. It is named MaxVectorDimensions in the config
-- package, which is the source of truth; a live-tier test asserts this
-- declaration still agrees with it.
--
-- There is no index: the server does not support extension-defined index
-- types, so every search is a sequential scan.
CREATE TABLE IF NOT EXISTS embeddings (
  chunk_id  BIGINT        NOT NULL,
  embedding SVECTOR(3072) NOT NULL,

  PRIMARY KEY (chunk_id),
  CONSTRAINT embeddings_chunk_fk FOREIGN KEY (chunk_id)
    REFERENCES chunks (id) ON DELETE CASCADE
) ENGINE = InnoDB;

-- One row per question asked, whatever the outcome. No de-duplication: a
-- question asked five times is five visitors who were told something.
--
-- recent_turns holds the last few messages of the conversation the question
-- arrived in, so an administrator reading an unanswered question can see what
-- was being discussed. It is not the whole conversation, which this system
-- never stores.
CREATE TABLE IF NOT EXISTS query_log (
  id           BIGINT       NOT NULL AUTO_INCREMENT,
  question     TEXT         NOT NULL,
  recent_turns JSON         NULL,
  outcome      VARCHAR(16)  NOT NULL,
  reason_code  VARCHAR(64)  NULL,
  model_used   VARCHAR(128) NULL,
  latency_ms   INT          NULL,
  trace_id     VARCHAR(64)  NULL,
  created_at   DATETIME(6)  NOT NULL,

  PRIMARY KEY (id),
  KEY query_log_outcome_idx (outcome, created_at DESC),
  KEY query_log_created_idx (created_at DESC),
  CONSTRAINT query_log_outcome_known CHECK (outcome IN ('answered', 'declined', 'errored'))
) ENGINE = InnoDB;

-- What retrieval considered for one question, one row per chunk.
--
-- The original stored two parallel arrays, which MySQL has no type for. A
-- child table is what the vendor's migration guide prescribes, and it is
-- better: the arrays relied on a positional correspondence nothing enforced.
--
-- chunk_id deliberately carries no foreign key. Publishing deletes the pages
-- it replaced, and with them their chunks; a cascade would erase the record of
-- what was retrieved, and a restrict would refuse the publish outright.
CREATE TABLE IF NOT EXISTS query_log_retrieval (
  query_log_id  BIGINT NOT NULL,
  rank_position INT    NOT NULL,
  chunk_id      BIGINT NOT NULL,
  score         DOUBLE NOT NULL,

  PRIMARY KEY (query_log_id, rank_position),
  KEY query_log_retrieval_chunk_idx (chunk_id),
  CONSTRAINT query_log_retrieval_log_fk FOREIGN KEY (query_log_id)
    REFERENCES query_log (id) ON DELETE CASCADE
) ENGINE = InnoDB;

-- At most one row, enforced by the primary key and the check constraint
-- together. The seed at the end of this file makes it exactly one.
CREATE TABLE IF NOT EXISTS system_state (
  id                SMALLINT    NOT NULL DEFAULT 1,
  halted            BOOLEAN     NOT NULL DEFAULT FALSE,
  session_epoch     INT         NOT NULL DEFAULT 0,
  fetch_lock        VARCHAR(64) NULL,
  fetch_started_at  DATETIME(6) NULL,
  last_fetched_at   DATETIME(6) NULL,
  last_published_at DATETIME(6) NULL,

  PRIMARY KEY (id),
  CONSTRAINT system_state_single_row CHECK (id = 1)
) ENGINE = InnoDB;

-- Both safe to repeat. The migration record takes its time from UTC_TIMESTAMP
-- rather than a column default, because that function is explicit about the
-- zone where CURRENT_TIMESTAMP is not.
INSERT INTO system_state (id) VALUES (1)
  ON DUPLICATE KEY UPDATE id = id;

INSERT INTO schema_migrations (version, name, applied_at)
  VALUES (1, '0001_schema', UTC_TIMESTAMP(6))
  ON DUPLICATE KEY UPDATE version = version;
