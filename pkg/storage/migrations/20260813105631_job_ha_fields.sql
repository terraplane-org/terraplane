-- Modify "jobs" table for durable enqueue / HA claim.
-- IF NOT EXISTS keeps this safe if an earlier local migration already added these columns.
ALTER TABLE "public"."jobs"
  ADD COLUMN IF NOT EXISTS "agent_id" character varying(255) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS "action" character varying(32) NOT NULL DEFAULT 'plan',
  ADD COLUMN IF NOT EXISTS "payload" jsonb NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS "lease_expires_at" timestamptz NULL;
-- New rows must set agent_id / action explicitly
ALTER TABLE "public"."jobs" ALTER COLUMN "agent_id" DROP DEFAULT;
ALTER TABLE "public"."jobs" ALTER COLUMN "action" DROP DEFAULT;
-- Create indexes
CREATE INDEX IF NOT EXISTS "idx_job_agent_status" ON "public"."jobs" ("agent_id", "status");
CREATE INDEX IF NOT EXISTS "idx_job_lease" ON "public"."jobs" ("lease_expires_at");
CREATE INDEX IF NOT EXISTS "idx_job_pending" ON "public"."jobs" ("repo", "stack_name", "action", "status");
