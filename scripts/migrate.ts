import { createPool } from '@reviewpilot/persistence';
import { migrate } from '@reviewpilot/persistence/migrate';
if (!process.env.DATABASE_URL) throw new Error('DATABASE_URL is required');
const pool = createPool(process.env.DATABASE_URL);
try {
  await migrate(pool);
  console.info('Database migrations applied.');
} finally {
  await pool.end();
}
