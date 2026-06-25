-- Create "jobs" table
CREATE TABLE "public"."jobs" (
  "id" character varying(32) NOT NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "repo" character varying(255) NOT NULL,
  "pr_number" integer NOT NULL,
  "stack_name" character varying(255) NOT NULL,
  "dir" character varying(512) NOT NULL,
  "commit_sha" character varying(64) NOT NULL,
  "status" character varying(32) NOT NULL,
  "output" text NULL,
  "error_msg" text NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_job_repo_pr" to table: "jobs"
CREATE INDEX "idx_job_repo_pr" ON "public"."jobs" ("repo", "pr_number");
-- Create "project_locks" table
CREATE TABLE "public"."project_locks" (
  "id" bigserial NOT NULL,
  "created_at" timestamptz NULL,
  "updated_at" timestamptz NULL,
  "repo" character varying(255) NOT NULL,
  "stack_name" character varying(255) NOT NULL,
  "workspace" character varying(64) NOT NULL DEFAULT 'default',
  "dir" character varying(512) NOT NULL,
  "pr_number" integer NOT NULL,
  "commit_sha" character varying(64) NOT NULL,
  "locked_by" character varying(255) NOT NULL,
  PRIMARY KEY ("id")
);
-- Create index "idx_project_lock" to table: "project_locks"
CREATE UNIQUE INDEX "idx_project_lock" ON "public"."project_locks" ("repo", "stack_name", "workspace");
