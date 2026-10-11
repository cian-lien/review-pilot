import { closeSync, openSync, readFileSync, readSync } from 'node:fs';
import { parseEnv } from 'node:util';
export function workerEnvironment(role, source = process.env) {
  if (!['review', 'evolution'].includes(role)) throw new Error('INVALID_WORKER_ROLE');
  const isolated = source.REVIEW_TEST_ISOLATED === '1';
  let local = {};
  if (!isolated) {
    try {
      local = parseEnv(readFileSync(new URL('../../../.env', import.meta.url), 'utf8'));
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
  }
  const values = { ...local, ...source };
  const serviceToken =
    role === 'review' ? 'REVIEW_WORKER_SERVICE_TOKEN' : 'EVOLUTION_WORKER_SERVICE_TOKEN';
  const allowed = [
    'PATH',
    'NODE_ENV',
    'LANG',
    'TMPDIR',
    'TMP',
    'TEMP',
    'RABBITMQ_URL',
    'INTERNAL_API_URL',
    'OTEL_EXPORTER_OTLP_ENDPOINT',
    'OTEL_SERVICE_NAME',
    'HEARTBEAT_FILE',
    serviceToken,
  ];
  if (role === 'review')
    allowed.push('REVIEW_MODE', 'REVIEW_REAL_ALLOWED', 'REVIEW_MODEL_CONFIG_FILE', 'MODEL_API_KEY');
  if (role === 'review' && values.REVIEW_MODE === 'fake' && isolated)
    allowed.push(
      'REVIEW_TEST_ISOLATED',
      'REVIEW_FIXTURE_ID',
      'TEST_MODEL_BASE_URL',
      'REVIEW_FAKE_SCRIPT_FILE',
    );
  const env = {};
  for (const name of allowed) {
    if (values[name] !== undefined) env[name] = values[name];
    const file = values[name + '_FILE'];
    if (
      file &&
      ['RABBITMQ_URL', serviceToken, ...(role === 'review' ? ['MODEL_API_KEY'] : [])].includes(name)
    ) {
      const fd = openSync(file, 'r');
      try {
        const data = Buffer.alloc(65537);
        const bytes = readSync(fd, data, 0, data.length, null);
        const value = data.subarray(0, bytes).toString('utf8').trim();
        if (bytes > 65536 || !value) throw new Error('INVALID_SECRET_FILE');
        env[name] = value;
      } finally {
        closeSync(fd);
      }
    }
  }
  return env;
}
