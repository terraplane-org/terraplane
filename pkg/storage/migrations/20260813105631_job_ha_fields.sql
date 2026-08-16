-- Modify "jobs" table for durable enqueue / HA claim
ALTER TABLE "public"."jobs"
  ADD COLUMN "agent_id" character varying(255) NOT NULL DEFAULT '',
  ADD COLUMN "action" character varying(32) NOT NULL DEFAULT 'plan',
  ADD COLUMN "payload" jsonb NOT NULL DEFAULT '{}',
  ADD COLUMN "lease_expires_at" timestamptz NULL;
-- New rows must set agent_id / action explicitly
ALTER TABLE "public"."jobs" ALTER COLUMN "agent_id" DROP DEFAULT;
ALTER TABLE "public"."jobs" ALTER COLUMN "action" DROP DEFAULT;
-- Create indexes
CREATE INDEX "idx_job_agent_status" ON "public"."jobs" ("agent_id", "status");
CREATE INDEX "idx_job_lease" ON "public"."jobs" ("lease_expires_at");
CREATE INDEX "idx_job_pending" ON "public"."jobs" ("repo", "stack_name", "action", "status");
