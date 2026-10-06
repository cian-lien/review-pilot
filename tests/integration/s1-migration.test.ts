import { expect, it } from 'vitest';
import { runScenario } from '../../scripts/s1-migration-test';

const selected = process.env.MIGRATION_CASE ?? 'all';
if (selected === 'all' || selected === 'legacy-schema') {
  it('Go preserves legacy schema and rejects changed migrations', async () => {
    expect(await runScenario('legacy-schema')).toMatchObject({
      runIdPreserved: true,
      sessionCiphertextPreserved: true,
      parallelTablesCreated: false,
      noDuplicateMigrationRows: true,
      changedMigrationRejected: true,
    });
  });
}
