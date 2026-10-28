-- Serialize plan status publication for one repository and commit.
CREATE TABLE "public"."plan_status_locks" (
  "repo" character varying(255) NOT NULL,
  "sha" character varying(64) NOT NULL,
  "updated_at" timestamptz NULL,
  PRIMARY KEY ("repo", "sha")
);
