ALTER TABLE jobs DROP CONSTRAINT jobs_status_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_status_check CHECK(status IN('queued','running','result_ready','completed','cancelled','failed'));
ALTER TABLE jobs ADD COLUMN notification_generation integer NOT NULL DEFAULT 0 CHECK(notification_generation>=0);
ALTER TABLE jobs ADD COLUMN execution_epoch bigint NOT NULL DEFAULT 0 CHECK(execution_epoch>=0);
ALTER TABLE sessions ADD COLUMN csrf_token text;

CREATE TABLE runtime_control (
 id integer PRIMARY KEY CHECK(id=1),
 mode text NOT NULL CHECK(mode IN('legacy','go')),
 epoch bigint NOT NULL DEFAULT 0 CHECK(epoch>=0)
);
INSERT INTO runtime_control(id,mode,epoch) VALUES(1,'legacy',0);

CREATE TABLE outbox_events (
 id uuid PRIMARY KEY,
 task_id uuid NOT NULL REFERENCES jobs(id),
 notification_generation integer NOT NULL CHECK(notification_generation>=0),
 kind text NOT NULL CHECK(kind IN('connection-check','evolution')),
 payload jsonb NOT NULL,
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 sent_at timestamptz,
 relay_token uuid,
 relay_expires_at timestamptz,
 UNIQUE(task_id,kind,notification_generation)
);
CREATE INDEX outbox_ready_idx ON outbox_events(available_at) WHERE sent_at IS NULL;

CREATE TABLE job_results (
 task_id uuid PRIMARY KEY REFERENCES jobs(id),
 result_id text NOT NULL UNIQUE,
 result_hash text NOT NULL CHECK(result_hash ~ '^[a-f0-9]{64}$'),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 payload jsonb NOT NULL,
 accepted_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE publication_requests (
 run_id uuid PRIMARY KEY REFERENCES review_runs(id),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','running','published','cancelled','failed')),
 lease_token uuid,
 lease_expires_at timestamptz,
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 last_error_code text,
 CHECK(state<>'running' OR(lease_token IS NOT NULL AND lease_expires_at IS NOT NULL))
);
CREATE INDEX publication_ready_idx ON publication_requests(available_at) WHERE state IN('pending','running');
