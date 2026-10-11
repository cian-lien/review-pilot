import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../', import.meta.url));
const env = {
  PATH: process.env.PATH,
  HOME: process.env.HOME,
  APP_HOST: 'reviewpilot.example.test',
  SECRET_DIRECTORY: '/tmp/reviewpilot-configuration-only',
  GITHUB_APP_ID: '999',
  GITHUB_APP_SLUG: 'fixture',
  GITHUB_CLIENT_ID: 'fixture',
  GRAFANA_ADMIN_PASSWORD: 'configuration-only-not-a-credential',
  OTEL_EXPORTER_OTLP_ENDPOINT: '',
  NEXT_TELEMETRY_DISABLED: '1',
};
for (const mode of ['dev', 'prod']) {
  const output = execFileSync(
    'docker',
    [
      'compose',
      '--env-file',
      '.env.example',
      '-f',
      'infra/compose.' + mode + '.yml',
      '--profile',
      'monitoring',
      'config',
      '--format',
      'json',
    ],
    { cwd: root, env, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] },
  );
  const config = JSON.parse(output);
  for (const [name, service] of Object.entries(config.services) as [
    string,
    {
      ports?: { host_ip?: string; target: number }[];
      networks: Record<string, unknown>;
      environment?: Record<string, string>;
      secrets?: unknown[];
    },
  ][]) {
    for (const port of service.ports ?? []) {
      assert(
        ![4001, 5432, 5672, 15672, 4318, 8889].includes(port.target),
        name + ' exposed a private port',
      );
      assert(
        (name === 'caddy' && mode === 'prod') || port.host_ip === '127.0.0.1',
        name + ' exposed a debug port',
      );
    }
    if (name.startsWith('worker-')) {
      assert(!('data' in service.networks), 'Worker must not join database network');
      assert(
        !Object.keys(service.environment ?? {}).some((key) => /DATABASE|GITHUB|SESSION/.test(key)),
        'Worker inherited backend secrets',
      );
    }
  }
  assert.equal(
    config.volumes.reviewpilot_pg.name,
    mode === 'dev' ? 'review-pilot_reviewpilot_pg' : 'reviewpilot_beta_pg',
  );
  if (mode === 'prod') assert(config.services.api.secrets.length >= 6);
  for (const name of ['api', 'dispatcher', 'worker-review']) {
    assert.equal(config.services[name].environment.REVIEW_MODE, 'disabled');
    assert.equal(config.services[name].environment.REVIEW_REAL_ALLOWED, 'false');
  }
  for (const name of ['api', 'dispatcher', 'web', 'worker-evolution']) {
    const service = config.services[name];
    assert(!Object.keys(service.environment ?? {}).some((key) => key.startsWith('MODEL_API_KEY')));
    assert(
      !(service.secrets ?? []).some(
        (secret: { source?: string }) => secret.source === 'model_api_key',
      ),
    );
    assert(
      !(service.volumes ?? []).some(
        (volume: { target?: string }) => volume.target === '/run/secrets/model_api_key',
      ),
    );
  }
}
const caddy = readFileSync(root + '/infra/caddy/Caddyfile', 'utf8');
assert(caddy.includes('respond @internal 404'));
assert(!caddy.includes('api:4001'));
const collector = readFileSync(root + '/infra/otel/collector.yaml', 'utf8');
assert(collector.includes('http://loki:3100/otlp'));
assert(collector.includes('http://tempo:4318'));
assert(readFileSync(root + '/infra/loki/loki.yaml', 'utf8').includes('retention_period: 168h'));
assert(readFileSync(root + '/infra/tempo/tempo.yaml', 'utf8').includes('block_retention: 168h'));
execFileSync(
  'docker',
  [
    'run',
    '--rm',
    '--network',
    'none',
    '--read-only',
    '--cap-drop=ALL',
    '--security-opt=no-new-privileges',
    '-v',
    root + '/infra/otel/collector.yaml:/etc/otelcol/config.yaml:ro',
    'otel/opentelemetry-collector-contrib:0.161.0',
    'validate',
    '--config=/etc/otelcol/config.yaml',
  ],
  { stdio: 'inherit' },
);
execFileSync(
  'docker',
  [
    'run',
    '--rm',
    '--network',
    'none',
    '--read-only',
    '--cap-drop=ALL',
    '--entrypoint',
    'promtool',
    '-v',
    root + '/infra/prometheus:/etc/prometheus:ro',
    'prom/prometheus:v3.14.0',
    'check',
    'config',
    '/etc/prometheus/prometheus.yml',
  ],
  { stdio: 'inherit' },
);
execFileSync(
  'docker',
  [
    'run',
    '--rm',
    '--network',
    'none',
    '--read-only',
    '--cap-drop=ALL',
    '--cap-add=NET_BIND_SERVICE',
    '-v',
    root + '/infra/caddy/Caddyfile:/etc/caddy/Caddyfile:ro',
    'caddy:2.11.7-alpine',
    'caddy',
    'validate',
    '--config',
    '/etc/caddy/Caddyfile',
    '--adapter',
    'caddyfile',
  ],
  { stdio: 'inherit' },
);
for (const [image, path, args] of [
  ['grafana/loki:3.7.8', 'loki/loki.yaml', ['-config.file=/config.yaml', '-verify-config=true']],
  [
    'grafana/tempo:3.1.0',
    'tempo/tempo.yaml',
    ['-config.file=/config.yaml', '-config.verify=true', '-config.verify-errors-only=true'],
  ],
] as [string, string, string[]][]) {
  execFileSync(
    'docker',
    [
      'run',
      '--rm',
      '--network',
      'none',
      '--read-only',
      '--cap-drop=ALL',
      '-v',
      root + '/infra/' + path + ':/config.yaml:ro',
      image,
      ...args,
    ],
    { stdio: 'inherit' },
  );
}
console.log(
  'Compose 隔离、旧卷映射、路由、保留期以及 Collector/Prometheus/Caddy 配置校验通过；未部署生产。',
);
