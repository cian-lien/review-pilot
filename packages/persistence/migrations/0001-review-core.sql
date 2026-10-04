CREATE TABLE installations (id bigint PRIMARY KEY,active boolean NOT NULL DEFAULT true,updated_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE repositories (
 installation_id bigint NOT NULL REFERENCES installations(id),id bigint NOT NULL,full_name text NOT NULL,
 public boolean NOT NULL,enabled boolean NOT NULL DEFAULT false,auth_revision integer NOT NULL DEFAULT 1,config_version integer NOT NULL DEFAULT 1,
 PRIMARY KEY(installation_id,id),CHECK(NOT enabled OR public)
);
CREATE TABLE pull_requests (
 installation_id bigint NOT NULL,repository_id bigint NOT NULL,number integer NOT NULL CHECK(number>0),
 base_sha text NOT NULL,head_sha text NOT NULL,remote_updated_at timestamptz NOT NULL,
 PRIMARY KEY(installation_id,repository_id,number),FOREIGN KEY(installation_id,repository_id) REFERENCES repositories(installation_id,id)
);
CREATE TABLE review_runs (
 id uuid PRIMARY KEY,installation_id bigint NOT NULL,repository_id bigint NOT NULL,pr_number integer NOT NULL CHECK(pr_number>0),
 base_sha text NOT NULL,head_sha text NOT NULL,pipeline_version text NOT NULL,generation integer NOT NULL CHECK(generation>=0),snapshot jsonb NOT NULL,
 status text NOT NULL DEFAULT 'Queued',check_id bigint,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(installation_id,repository_id) REFERENCES repositories(installation_id,id),
 UNIQUE(installation_id,repository_id,pr_number,head_sha,pipeline_version,generation),
 CHECK(status IN('Queued','Indexing','Reviewing','Verifying','Publishing','Completed','Partial','Stale','Cancelled','Failed'))
);
CREATE TABLE jobs (
 id uuid PRIMARY KEY,run_id uuid NOT NULL UNIQUE REFERENCES review_runs(id),kind text NOT NULL DEFAULT 'review' CHECK(kind IN('review','evolution')),
 status text NOT NULL DEFAULT 'queued' CHECK(status IN('queued','running','completed','cancelled','failed')),attempts integer NOT NULL DEFAULT 0,max_attempts integer NOT NULL DEFAULT 5,
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),lease_token uuid,lease_expires_at timestamptz,worker_id text,last_error_code text,
 CHECK(attempts>=0 AND max_attempts>0),CHECK(status<>'running' OR(lease_token IS NOT NULL AND lease_expires_at IS NOT NULL))
);
CREATE INDEX jobs_claim_idx ON jobs(kind,status,available_at);
CREATE TABLE stage_checkpoints (run_id uuid NOT NULL REFERENCES review_runs(id),stage text NOT NULL,artifact_ref text,input_hash text NOT NULL,status text NOT NULL CHECK(status IN('pending','completed','failed')),PRIMARY KEY(run_id,stage));
CREATE TABLE webhook_deliveries (delivery_id text PRIMARY KEY,event text NOT NULL,payload_hash text NOT NULL,received_at timestamptz NOT NULL DEFAULT clock_timestamp());
