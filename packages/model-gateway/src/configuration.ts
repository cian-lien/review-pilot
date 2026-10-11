import { closeSync, openSync, readSync } from 'node:fs';
import { z } from 'zod';
import type { ModelConfiguration } from './types';
import { createHash } from 'node:crypto';
export function modelConfigHash(configuration: ModelConfiguration): string {
  const value = configurationSchema.parse(configuration);
  // Match the fixed Go configuration field order and encoding/json escaping.
  const body = JSON.stringify(value).replace(
    /[<>&\u2028\u2029]/g,
    (c) => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0'),
  );
  return createHash('sha256').update(body).digest('hex');
}
const version = z.string().regex(/^[a-zA-Z0-9._-]{1,128}$/);
const money = z
  .string()
  .regex(/^[1-9][0-9]{0,18}$/)
  .refine((v) => BigInt(v) <= 9223372036854775807n);
export const configurationSchema = z.strictObject({
  modelConfigVersion: version,
  priceVersion: version,
  baseURL: z.url().max(2048),
  model: z.string().min(1).max(128),
  structuredOutputMode: z.enum(['json-schema', 'json-object']),
  currency: z.string().regex(/^(TEST|[A-Z]{3})$/),
  inputMicrosPerMillion: money,
  outputMicrosPerMillion: money,
  runLimitMicros: money,
  repositoryDailyLimitMicros: money,
  globalMonthlyLimitMicros: money,
  usageSupported: z.literal(true),
  onlyTokenBilling: z.literal(true),
  fixtureId: z.string().max(128),
});
export function readBoundedFile(path: string, limit: number): string {
  const fd = openSync(path, 'r');
  try {
    const data = Buffer.alloc(limit + 1);
    const bytes = readSync(fd, data, 0, data.length, null);
    if (bytes > limit) throw new Error('CONFIG_TOO_LARGE');
    return new TextDecoder('utf-8', { fatal: true }).decode(data.subarray(0, bytes));
  } finally {
    closeSync(fd);
  }
}
export function readModelConfiguration(path: string): ModelConfiguration {
  try {
    return Object.freeze(configurationSchema.parse(JSON.parse(readBoundedFile(path, 16384))));
  } catch {
    throw new Error('INVALID_MODEL_CONFIG');
  }
}
