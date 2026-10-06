import { expect, it } from 'vitest';
import { runScenario } from '../../scripts/s1-migration-test';

const selected = process.env.MIGRATION_CASE ?? 'all';
if (selected === 'all' || selected === 'cutover') {
  it('cutover preserves facts and refuses unconfirmed handover', async () => {
    expect(await runScenario('cutover')).toMatchObject({
      switchWithActiveLeaseRejected: true,
      pendingTaskIdPreserved: true,
      completedRenotified: false,
      unreconciledPublishBlocksSwitch: true,
      staleEpochStatus: 409,
      rollbackLeavesTwoConsumers: false,
    });
  });
}
if (selected === 'all' || selected === 'observability') {
  it('real Collector correlates Go, AMQP and Worker without leaking credentials', async () => {
    expect(await runScenario('observability')).toMatchObject({
      linkedServices: ['api', 'dispatcher', 'worker-review'],
      secretsCaptured: false,
      completedWithCollectorDown: true,
      publicInternalRouteStatus: 404,
    });
  });
}
if (selected === 'all' || selected === 'webhook-check') {
  it('signed events and fenced Check publication preserve S1 semantics', async () => {
    expect(await runScenario('webhook-check')).toMatchObject({
      invalidSignatureStatus: 401,
      oversizedStatus: 413,
      deliveryConflictStatus: 409,
      logicalRunCount: 1,
      logicalOutboxCount: 1,
      checkCreateCountAfterLostResponse: 1,
      staleHeadPublished: false,
      revokedPublished: false,
      conclusion: 'neutral',
    });
  });
}
if (selected === 'all' || selected === 'message-recovery') {
  it('messages recover without duplicate execution or early acknowledgement', async () => {
    expect(await runScenario('message-recovery')).toMatchObject({
      activeExecutions: 1,
      acceptedResultRows: 1,
      oldLeaseStatus: 409,
      changedResultStatus: 409,
      recoveredByRead: true,
      acknowledgedBeforeCommit: false,
    });
  });
}
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
