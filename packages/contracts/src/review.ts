import Ajv2020 from 'ajv/dist/2020.js';
import source from '../schemas/review-api.gen.json';
import type { components } from './s1.gen';

export type ReviewPolicy = components['schemas']['ReviewPolicy'];
export type ReviewSnapshot = components['schemas']['ReviewSnapshot'];
export type ReviewLease = components['schemas']['ReviewLease'];
export type ReviewContextPage = components['schemas']['ReviewContextPage'];
export type ReviewChangedFile = components['schemas']['ReviewChangedFile'];
export type EvidenceRef = components['schemas']['EvidenceRef'];
export type CodeEvidence = components['schemas']['CodeEvidence'];
export type ReviewCandidate = components['schemas']['ReviewCandidate'];
export type VerificationDecision = components['schemas']['VerificationDecision'];
export type ModelCall = components['schemas']['ModelCall'];
export type ReviewStageRecord = components['schemas']['ReviewStageRecord'];
export type ReviewResult = components['schemas']['ReviewResult'];
export type ReviewRunDetail = components['schemas']['ReviewRunDetail'];
export type ReviewPullList = components['schemas']['ReviewPullList'];
export type ReviewRunList = components['schemas']['ReviewRunList'];
export type ReviewPublicationPlan = components['schemas']['ReviewPublicationPlan'];
export type ReviewPublicationReceipt = components['schemas']['ReviewPublicationReceipt'];
export type ReviewSchemaName = keyof components['schemas'];

function project(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(project);
  if (!value || typeof value !== 'object') return value;
  const original = value as Record<string, unknown>;
  const output: Record<string, unknown> = {};
  for (const [key, child] of Object.entries(original)) {
    if (key === 'nullable' || key === 'format' || key.startsWith('x-')) continue;
    output[key] =
      key === '$ref' && typeof child === 'string'
        ? child.replace('#/components/schemas/', '#/$defs/')
        : project(child);
  }
  return original.nullable ? { anyOf: [output, { type: 'null' }] } : output;
}
const definitions = project(source.components.schemas);
const validators = new Map<string, ReturnType<Ajv2020['compile']>>();
function validateMoney(value: unknown): void {
  if (!value || typeof value !== 'object') return;
  for (const [key, child] of Object.entries(value)) {
    if (key.endsWith('Micros') && typeof child === 'string' && BigInt(child) > 9223372036854775807n)
      throw new Error('INVALID_REVIEW_AMOUNT');
    validateMoney(child);
  }
}
export function validateReview<N extends ReviewSchemaName>(
  name: N,
  value: unknown,
): components['schemas'][N] {
  let validate = validators.get(name);
  if (!validate) {
    validate = new Ajv2020({ strict: true, allErrors: false }).compile({
      $ref: '#/$defs/' + name,
      $defs: definitions,
    });
    validators.set(name, validate);
  }
  if (!validate(value)) throw new Error('INVALID_REVIEW_' + name);
  validateMoney(value);
  return value as components['schemas'][N];
}
