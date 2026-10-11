ALTER TABLE publication_requests DROP CONSTRAINT publication_requests_state_check;
ALTER TABLE publication_requests ADD CONSTRAINT publication_requests_state_check CHECK(state IN('pending','running','published','cancelled','failed','uncertain'));
ALTER TABLE publication_requests ADD COLUMN review_plan jsonb;
ALTER TABLE publication_requests ADD COLUMN review_write_started boolean NOT NULL DEFAULT false;
ALTER TABLE publication_requests ADD COLUMN check_write_started boolean NOT NULL DEFAULT false;
ALTER TABLE publication_requests ADD COLUMN review_plan_erased boolean NOT NULL DEFAULT false;
ALTER TABLE review_findings ADD COLUMN publication_reason text;

-- Slot identity survives Run/SHA changes and content deletion; unknown writes retain occupancy.
CREATE TABLE pr_review_slots (
 installation_id bigint NOT NULL,
 repository_id bigint NOT NULL,
 pr_number integer NOT NULL,
 fingerprint text NOT NULL CHECK(fingerprint ~ '^[a-f0-9]{64}$'),
 run_id uuid,
 state text NOT NULL CHECK(state IN('reserved','published','uncertain')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(installation_id,repository_id,pr_number,fingerprint)
);
CREATE TABLE review_publication_facts (
 run_id uuid PRIMARY KEY REFERENCES review_runs(id),
 plan_hash text NOT NULL,
 receipt jsonb NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
