CREATE TABLE review_model_configs (
 version text PRIMARY KEY, digest text NOT NULL CHECK(digest ~ '^[a-f0-9]{64}$'), payload jsonb NOT NULL
);
CREATE TABLE budget_limits (
 scope_type text NOT NULL CHECK(scope_type IN('run','repository','global')), scope_key text NOT NULL, currency text NOT NULL,
 limit_micros bigint NOT NULL CHECK(limit_micros>0), held_micros bigint NOT NULL DEFAULT 0 CHECK(held_micros>=0),
 spent_micros bigint NOT NULL DEFAULT 0 CHECK(spent_micros>=0), PRIMARY KEY(scope_type,scope_key,currency)
);
CREATE TABLE budget_reservations (
 id uuid PRIMARY KEY, run_id uuid REFERENCES review_runs(id), installation_id bigint, repository_id bigint,
 currency text NOT NULL, reserved_micros bigint NOT NULL CHECK(reserved_micros>0), scopes jsonb NOT NULL,
 state text NOT NULL DEFAULT 'held' CHECK(state IN('held','settled','released')), actual_micros bigint CHECK(actual_micros>=0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE model_calls (
 call_id uuid PRIMARY KEY, reservation_id uuid NOT NULL UNIQUE REFERENCES budget_reservations(id), run_id uuid REFERENCES review_runs(id),
 purpose text NOT NULL CHECK(purpose IN('generate','verify')), ordinal integer NOT NULL CHECK(ordinal BETWEEN 1 AND 2),
 request_hash text NOT NULL, state text NOT NULL CHECK(state IN('reserved','dispatching','settled','released','uncertain')),
 model_config_version text NOT NULL REFERENCES review_model_configs(version), price_version text NOT NULL, currency text NOT NULL,
 input_price bigint NOT NULL CHECK(input_price>0), output_price bigint NOT NULL CHECK(output_price>0),
 input_limit integer NOT NULL CHECK(input_limit>0 AND input_limit<=12000), output_limit integer NOT NULL CHECK(output_limit>0 AND output_limit<=4096),
 reserved_cost_micros bigint NOT NULL CHECK(reserved_cost_micros>0), actual_cost_micros bigint CHECK(actual_cost_micros>=0), usage jsonb,
 response_hash text, outcome text, reason_code text, revision integer NOT NULL DEFAULT 0 CHECK(revision>=0),
 retry_authorized boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(run_id,purpose,ordinal)
);
CREATE TABLE review_stage_artifacts (
 run_id uuid NOT NULL REFERENCES review_runs(id), stage text NOT NULL CHECK(stage IN('context','candidates','verification')),
 stage_input_hash text NOT NULL CHECK(stage_input_hash ~ '^[a-f0-9]{64}$'), output_hash text NOT NULL CHECK(output_hash ~ '^[a-f0-9]{64}$'),
 call_id uuid REFERENCES model_calls(call_id), payload jsonb, expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(run_id,stage,stage_input_hash),
 CHECK(octet_length(payload::text)<=262144)
);
CREATE TABLE operator_audits (
 id uuid PRIMARY KEY, action text NOT NULL, call_id uuid, expected_revision integer, outcome text, receipt_ref text,
 retry_stage boolean NOT NULL DEFAULT false, installation_id bigint, repository_id bigint,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX review_stage_expiry_idx ON review_stage_artifacts(expires_at);
