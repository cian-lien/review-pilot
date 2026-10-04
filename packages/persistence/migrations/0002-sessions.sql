CREATE TABLE users (id bigint PRIMARY KEY,login text NOT NULL);
CREATE TABLE installation_memberships (user_id bigint NOT NULL REFERENCES users(id),installation_id bigint NOT NULL REFERENCES installations(id),PRIMARY KEY(user_id,installation_id));
CREATE TABLE sessions (token_hash text PRIMARY KEY,user_id bigint NOT NULL REFERENCES users(id),encrypted_user_token text NOT NULL,expires_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE INDEX sessions_expiry_idx ON sessions(expires_at);
CREATE TABLE oauth_attempts (state_hash text PRIMARY KEY,verifier text NOT NULL,expires_at timestamptz NOT NULL);
