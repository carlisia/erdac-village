-- 0002 - record when a fetch takes over an abandoned lock.
--
-- A fetch that crashes leaves its lock behind. After the expiry a new fetch may
-- take the lock over, which keeps a crashed run from locking the system out.
-- But the crashed run's pages are still waiting for review, half of one crawl,
-- and once the new run finishes cleanly nothing in the row says a takeover ever
-- happened. This column says it. Publish refuses while it is set, and only a
-- fetch that ran in force mode and completed clears it, because only a force
-- run has demonstrably replaced every page.
--
-- Idempotent, like every migration here. MySQL has no ADD COLUMN IF NOT EXISTS,
-- so the catalogue is asked whether the column is there and the statement is
-- prepared only when it is not. Preparing a statement from a string is the
-- server's own mechanism and is why the ALTER appears inside one. The vendor's
-- migrations guide does not prescribe this shape, so PORTING.md records it as
-- a chosen divergence, measured 2026-09-09, rather than a vendor answer.
SET @present = (
  SELECT COUNT(*) FROM information_schema.columns
   WHERE table_schema = DATABASE()
     AND table_name = 'system_state'
     AND column_name = 'fetch_taken_over_at'
);
SET @ddl = IF(@present = 0,
  'ALTER TABLE system_state ADD COLUMN fetch_taken_over_at DATETIME(6) NULL',
  'SELECT 1');
PREPARE takeover FROM @ddl;
EXECUTE takeover;
DEALLOCATE PREPARE takeover;

INSERT INTO schema_migrations (version, name, applied_at)
  VALUES (2, '0002_fetch_takeover', UTC_TIMESTAMP(6))
  ON DUPLICATE KEY UPDATE version = version;
