-- Modify "jobs" table for agent polling / HA enqueue
ALTER TABLE "public"."jobs"
  ADD COLUMN "agent_id" character varying(255) NOT NULL DEFAULT '',
  ADD COLUMN "action" character varying(32) NOT NULL DEFAULT 'plan',
  ADD COLUMN "plan_flags" character varying(1024) NULL,
  ADD COLUMN "trigger_user" character varying(255) NULL,
  ADD COLUMN "lease_expires_at" timestamptz NULL;
-- Drop default after backfill column add (new rows must set explicitly)
ALTER TABLE "public"."jobs" ALTER COLUMN "agent_id" DROP DEFAULT;
ALTER TABLE "public"."jobs" ALTER COLUMN "action" DROP DEFAULT;
-- Create indexes
CREATE INDEX "idx_job_agent_status" ON "public"."jobs" ("agent_id", "status");
CREATE INDEX "idx_job_pending" ON "public"."jobs" ("repo", "stack_name", "action", "status");
CREATE INDEX "idx_job_lease" ON "public"."jobs" ("lease_expires_at");
