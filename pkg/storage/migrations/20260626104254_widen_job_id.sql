-- Modify "jobs" table
ALTER TABLE "public"."jobs" ALTER COLUMN "id" TYPE character varying(36), ALTER COLUMN "status" SET DEFAULT 'pending';
