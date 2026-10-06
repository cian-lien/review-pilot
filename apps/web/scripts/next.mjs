import { readFile } from 'node:fs/promises';
import { parseEnv } from 'node:util';

// CLI env-file flags are copied by Next dev into NODE_OPTIONS, where Node rejects them.
try {
  const values = parseEnv(await readFile(new URL('../../../.env', import.meta.url), 'utf8'));
  for (const key of ['NEXT_TELEMETRY_DISABLED', 'PORT', 'NODE_ENV']) {
    if (process.env[key] === undefined && values[key] !== undefined) process.env[key] = values[key];
  }
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}

// Next is a display process. Never inherit backend, provider, or worker credentials.
const allowed = new Set([
  'PATH',
  'HOME',
  'USER',
  'LOGNAME',
  'SHELL',
  'LANG',
  'TZ',
  'TMPDIR',
  'TMP',
  'TEMP',
  'NODE_ENV',
  'PORT',
  'HOSTNAME',
  'NEXT_TELEMETRY_DISABLED',
  'OTEL_EXPORTER_OTLP_ENDPOINT',
  'OTEL_SERVICE_NAME',
]);
for (const key of Object.keys(process.env)) {
  if (!allowed.has(key) && !key.startsWith('LC_')) delete process.env[key];
}

await import('next/dist/bin/next');
