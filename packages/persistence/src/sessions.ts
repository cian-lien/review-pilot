import { createHash, randomBytes } from 'node:crypto';
import type { Pool } from 'pg';
import { transaction } from './database';
const hash = (value: string) => createHash('sha256').update(value).digest('hex');
export async function saveOAuthAttempt(pool: Pool, state: string, verifier: string): Promise<void> {
  await pool.query('DELETE FROM oauth_attempts WHERE expires_at<clock_timestamp()');
  await pool.query(
    "INSERT INTO oauth_attempts(state_hash,verifier,expires_at) VALUES($1,$2,clock_timestamp()+interval '10 minutes')",
    [hash(state), verifier],
  );
}
export async function consumeOAuthAttempt(pool: Pool, state: string): Promise<string> {
  const result = await pool.query<{ verifier: string }>(
    'DELETE FROM oauth_attempts WHERE state_hash=$1 AND expires_at>clock_timestamp() RETURNING verifier',
    [hash(state)],
  );
  if (!result.rows[0]) throw new Error('OAuth state invalid or expired');
  return result.rows[0].verifier;
}
export async function createSession(
  pool: Pool,
  user: { id: number; login: string },
  encryptedToken: string,
  lifetimeSeconds: number,
): Promise<string> {
  if (!Number.isSafeInteger(lifetimeSeconds) || lifetimeSeconds < 1)
    throw new Error('Invalid session lifetime');
  const token = randomBytes(32).toString('base64url');
  await transaction(pool, async (client) => {
    await client.query(
      'INSERT INTO users(id,login) VALUES($1,$2) ON CONFLICT(id) DO UPDATE SET login=EXCLUDED.login',
      [user.id, user.login],
    );
    await client.query('DELETE FROM sessions WHERE expires_at<clock_timestamp()');
    await client.query(
      "INSERT INTO sessions(token_hash,user_id,encrypted_user_token,expires_at) VALUES($1,$2,$3,clock_timestamp()+$4*interval '1 second')",
      [hash(token), user.id, encryptedToken, Math.min(lifetimeSeconds, 28800)],
    );
  });
  return token;
}
export async function readSession(
  pool: Pool,
  token: string,
): Promise<{ userId: number; login: string; encryptedToken: string } | null> {
  if (!/^[a-zA-Z0-9_-]{43}$/.test(token)) return null;
  const result = await pool.query<{ user_id: string; login: string; encrypted_user_token: string }>(
    'SELECT s.user_id,u.login,s.encrypted_user_token FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>clock_timestamp()',
    [hash(token)],
  );
  const session = result.rows[0];
  return session
    ? {
        userId: Number(session.user_id),
        login: session.login,
        encryptedToken: session.encrypted_user_token,
      }
    : null;
}
export async function deleteSession(pool: Pool, token: string): Promise<void> {
  await pool.query('DELETE FROM sessions WHERE token_hash=$1', [hash(token)]);
}
