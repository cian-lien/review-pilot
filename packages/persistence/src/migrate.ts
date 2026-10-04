import { createHash } from 'node:crypto';
import { readdir, readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import type { Pool } from 'pg';
import { transaction } from './database';
// CLI-only entry. The Web server must not bundle migration files or expose schema-changing operations.
export async function migrate(pool: Pool): Promise<void> {
  const directory = fileURLToPath(new URL('../migrations/', import.meta.url));
  await transaction(pool, async (client) => {
    await client.query('SELECT pg_advisory_xact_lock(782914630)');
    await client.query(
      'CREATE TABLE IF NOT EXISTS schema_migrations(name text PRIMARY KEY,hash text NOT NULL,applied_at timestamptz NOT NULL DEFAULT clock_timestamp())',
    );
    for (const name of (await readdir(directory))
      .filter((name) => /^\d+.*\.sql$/.test(name))
      .sort()) {
      const source = await readFile(`${directory}/${name}`, 'utf8'),
        hash = createHash('sha256').update(source).digest('hex');
      const existing = await client.query<{ hash: string }>(
        'SELECT hash FROM schema_migrations WHERE name=$1',
        [name],
      );
      if (existing.rows[0]) {
        if (existing.rows[0].hash !== hash) throw new Error(`Applied migration changed: ${name}`);
        continue;
      }
      await client.query(source);
      await client.query('INSERT INTO schema_migrations(name,hash) VALUES($1,$2)', [name, hash]);
    }
  });
}
