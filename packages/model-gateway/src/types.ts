import type { components, ModelCall, ReviewLease } from '@reviewpilot/contracts';
type Schemas = components['schemas'];
export interface BudgetClient {
  reserve(request: Schemas['ReserveModelCallRequest']): Promise<ModelCall>;
  dispatch(request: Schemas['DispatchModelCallRequest']): Promise<ModelCall>;
  settle(request: Schemas['SettleModelCallRequest']): Promise<ModelCall>;
}
export interface ModelConfiguration {
  modelConfigVersion: string;
  priceVersion: string;
  baseURL: string;
  model: string;
  structuredOutputMode: 'json-schema' | 'json-object';
  currency: string;
  inputMicrosPerMillion: string;
  outputMicrosPerMillion: string;
  runLimitMicros: string;
  repositoryDailyLimitMicros: string;
  globalMonthlyLimitMicros: string;
  usageSupported: boolean;
  onlyTokenBilling: boolean;
  fixtureId: string;
}
export interface ProviderRequest {
  purpose: 'generate' | 'verify';
  system: string;
  input: string;
  schema: Record<string, unknown>;
  outputTokenLimit: number;
}
export interface ProviderResponse {
  data: unknown;
  usage?: Schemas['ModelUsage'];
  safeResponseHash: string;
  truncated?: boolean;
}
export interface ModelProvider {
  readonly configuration: ModelConfiguration;
  invoke(request: ProviderRequest, signal: AbortSignal): Promise<ProviderResponse>;
}
export interface ModelRequest<T> {
  lease: ReviewLease;
  purpose: 'generate' | 'verify';
  ordinal: 1 | 2;
  schema: Record<string, unknown>;
  validate(value: unknown): T;
  input: string;
  signal: AbortSignal;
}
export type ModelOutcome<T> =
  | { kind: 'succeeded'; callId: string; value: T }
  | { kind: 'failed' | 'needs-reconciliation'; callId: string; reasonCode: string };
export class ProviderFault extends Error {
  constructor(
    readonly reasonCode: string,
    readonly knownUsage?: Schemas['ModelUsage'],
  ) {
    super(reasonCode);
  }
}
