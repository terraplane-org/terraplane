-- Speed up deletion of old terminal jobs (succeeded/failed).
CREATE INDEX IF NOT EXISTS "idx_job_terminal_created_at" ON "public"."jobs" ("created_at")
  WHERE status IN ('succeeded', 'failed');
