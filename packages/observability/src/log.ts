export interface LogFields {
  service: string;
  requestId?: string;
  runId?: string;
  taskId?: string;
  attempt?: number;
  traceId?: string;
  stage?: string;
  code?: string;
  durationMs?: number;
}
const services = new Set(['api', 'dispatcher', 'web', 'worker-review', 'worker-evolution']);
const id = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
export function safeFields(fields: LogFields): Record<string, string | number> {
  const result: Record<string, string | number> = {};
  if (services.has(fields.service)) result.service = fields.service;
  for (const key of ['runId', 'taskId'] as const)
    if (typeof fields[key] === 'string' && id.test(fields[key]!)) result[key] = fields[key]!;
  for (const key of ['traceId', 'requestId'] as const)
    if (typeof fields[key] === 'string' && /^[a-f0-9]{32}$/.test(fields[key]!))
      result[key] = fields[key]!;
  if (typeof fields.stage === 'string' && /^[a-z][a-z0-9_.-]{0,63}$/.test(fields.stage))
    result.stage = fields.stage;
  if (typeof fields.code === 'string' && /^[A-Z_]{1,64}$/.test(fields.code))
    result.code = fields.code;
  for (const key of ['attempt', 'durationMs'] as const)
    if (
      typeof fields[key] === 'number' &&
      Number.isFinite(fields[key]) &&
      fields[key]! >= 0 &&
      fields[key]! <= 1e9
    )
      result[key] = fields[key]!;
  return result;
}
let sink: ((fields: Record<string, string | number>) => void) | undefined;
export function setLogSink(next: typeof sink): void {
  sink = next;
}
export function safeLog(fields: LogFields): void {
  const safe = safeFields(fields);
  console.info(JSON.stringify(safe));
  try {
    sink?.(safe);
  } catch {
    /* Telemetry never changes business outcomes. */
  }
}
