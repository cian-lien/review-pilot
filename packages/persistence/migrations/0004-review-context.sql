ALTER TABLE review_runs ADD COLUMN needs_reconciliation boolean NOT NULL DEFAULT false;
ALTER TABLE review_runs ADD COLUMN reason_code text;
ALTER TABLE review_runs ADD COLUMN provenance jsonb;
ALTER TABLE review_runs ADD COLUMN model_config_version text;
ALTER TABLE review_runs ADD COLUMN price_version text;
ALTER TABLE outbox_events DROP CONSTRAINT outbox_events_kind_check;
ALTER TABLE outbox_events ADD CONSTRAINT outbox_events_kind_check CHECK(kind IN('connection-check','evolution','ai-review'));

CREATE TABLE review_context_manifests (
 id uuid PRIMARY KEY, run_id uuid NOT NULL UNIQUE REFERENCES review_runs(id), input_hash text NOT NULL,
 files jsonb NOT NULL, pages jsonb NOT NULL DEFAULT '[]', coverage jsonb NOT NULL,
 next_index integer NOT NULL DEFAULT 0 CHECK(next_index>=0), sealed boolean NOT NULL DEFAULT false,
 context_hash text, expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(NOT sealed OR context_hash ~ '^[a-f0-9]{64}$')
);
CREATE TABLE review_evidence (
 id uuid PRIMARY KEY, run_id uuid NOT NULL REFERENCES review_runs(id), manifest_id uuid NOT NULL REFERENCES review_context_manifests(id),
 installation_id bigint NOT NULL, repository_id bigint NOT NULL, path text NOT NULL, sha text NOT NULL,
 content_hash text NOT NULL, start_line integer NOT NULL CHECK(start_line>0), end_line integer NOT NULL CHECK(end_line>=start_line),
 original_hash text NOT NULL, snippet text, redacted boolean NOT NULL DEFAULT false, expires_at timestamptz NOT NULL,
 FOREIGN KEY(installation_id,repository_id) REFERENCES repositories(installation_id,id), CHECK(octet_length(snippet)<=2048)
);
CREATE INDEX review_evidence_run_idx ON review_evidence(run_id);
CREATE INDEX review_context_expiry_idx ON review_context_manifests(expires_at);
