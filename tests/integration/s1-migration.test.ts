import { expect, it } from 'vitest';
import { runScenario } from '../../scripts/s1-migration-test';

const selected = process.env.MIGRATION_CASE ?? 'all';
if (selected === 'all' || selected === 'legacy-auth') {
  it('Go reads legacy sessions and enforces current authorization', async () => {
    expect(await runScenario('legacy-auth')).toMatchObject({
      decryptedToken: 'test-token',
      legacySessionStatus: 200,
      replayedStateStatus: 400,
      nonMaintainerStatus: 403,
      foreignInstallationStatus: 403,
      expiredSessionStatus: 401,
    });
  });
}
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
