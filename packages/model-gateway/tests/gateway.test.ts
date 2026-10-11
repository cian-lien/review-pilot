import { describe, it, expect } from 'vitest';
import { ModelGateway } from '../src/gateway';
import { ProviderFault, type BudgetClient, type ModelProvider } from '../src/types';
import { call, configuration, lease } from './fixture';
function setup(mode = 'success') {
  const trace: string[] = [];
  let invokes = 0;
  const budgets: BudgetClient = {
    async reserve(req) {
      trace.push('reserve');
      return {
        ...call,
        callId: req.ordinal === 1 ? call.callId : lease.leaseToken,
        ordinal: req.ordinal,
      };
    },
    async dispatch(req) {
      trace.push('dispatch');
      if (mode === 'dispatch-lost') throw new Error('LOST');
      return {
        ...call,
        callId: req.callId,
        state: 'dispatching',
        revision: 1,
        sendAuthorized: true,
      };
    },
    async settle(req) {
      trace.push('settle');
      return {
        ...call,
        callId: req.callId,
        state: req.outcome === 'uncertain' ? 'uncertain' : 'settled',
        revision: 2,
      };
    },
  };
  const provider: ModelProvider = {
    configuration,
    async invoke() {
      trace.push('invoke');
      invokes++;
      if (mode === 'timeout') throw new ProviderFault('MODEL_TIMEOUT');
      if (mode === 'retry' && invokes === 1)
        throw new ProviderFault('MODEL_RATE_LIMITED', { inputTokens: 0, outputTokens: 0 });
      return {
        data: mode === 'schema' ? null : { ok: true },
        usage: mode === 'missing-usage' ? undefined : { inputTokens: 3, outputTokens: 1 },
        safeResponseHash: 'c'.repeat(64),
        truncated: mode === 'truncated',
      };
    },
  };
  const gateway = new ModelGateway(provider, budgets);
  return {
    trace,
    get invokes() {
      return invokes;
    },
    gateway,
  };
}
const request = {
  lease,
  purpose: 'generate' as const,
  ordinal: 1 as const,
  schema: { type: 'object' },
  input: 'const value = 1;',
  signal: new AbortController().signal,
  validate(value: unknown) {
    if (!value || typeof value !== 'object') throw new Error('SCHEMA');
    return value;
  },
};
describe('budgeted model gateway', () => {
  it('authorizes dispatch before one invocation and settlement', async () => {
    const s = setup();
    expect((await s.gateway.call(request)).kind).toBe('succeeded');
    expect(s.trace).toEqual(['reserve', 'dispatch', 'invoke', 'settle']);
  });
  it.each(['dispatch-lost', 'timeout', 'missing-usage'])(
    'never repeats unknown execution: %s',
    async (mode) => {
      const s = setup(mode);
      expect((await s.gateway.call(request)).kind).toBe('needs-reconciliation');
      expect(s.invokes).toBe(mode === 'dispatch-lost' ? 0 : 1);
    },
  );
  it('reserves a distinct call for only one known retry', async () => {
    const s = setup('retry');
    expect((await s.gateway.call(request)).kind).toBe('succeeded');
    expect(s.invokes).toBe(2);
    expect(s.trace.filter((x) => x === 'reserve')).toHaveLength(2);
  });
  it.each(['schema', 'truncated'])(
    'rejects unusable known responses without retries: %s',
    async (mode) => {
      const s = setup(mode);
      expect((await s.gateway.call(request)).kind).toBe('failed');
      expect(s.invokes).toBe(1);
    },
  );
  it('refuses changed frozen configuration before reservation', async () => {
    const s = setup();
    await expect(
      s.gateway.call({
        ...request,
        lease: { ...lease, snapshot: { ...lease.snapshot, modelConfigVersion: 'changed' } },
      }),
    ).rejects.toThrow('MODEL_CONFIG_MISMATCH');
    expect(s.trace).toEqual([]);
  });
});
