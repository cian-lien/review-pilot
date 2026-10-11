CREATE TABLE review_findings (
 id uuid PRIMARY KEY,run_id uuid NOT NULL REFERENCES review_runs(id),fingerprint text NOT NULL,root_key jsonb NOT NULL,
 candidate jsonb,evidence jsonb,publication_status text NOT NULL DEFAULT 'pending' CHECK(publication_status IN('pending','published','not-published','uncertain')),
 reason_code text,expires_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(run_id,fingerprint)
);
CREATE TABLE review_filtered_candidates (
 run_id uuid NOT NULL REFERENCES review_runs(id),candidate_id text NOT NULL,title text,reason_code text NOT NULL,evidence jsonb,
 expires_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(run_id,candidate_id)
);
CREATE TABLE review_coverage (
 run_id uuid PRIMARY KEY REFERENCES review_runs(id),payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX review_finding_expiry_idx ON review_findings(expires_at);
