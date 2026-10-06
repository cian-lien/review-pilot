import { spawnSync } from 'node:child_process';
const [action, ...args] = process.argv.slice(2);
if (!['up', 'stop'].includes(action)) throw new Error('INVALID_INFRA_ACTION');
if (args.includes('--prod'))
  throw new Error('生产启动需要手册中的显式预检与单独授权；此入口只管理开发环境。');
const monitoring = args.includes('--monitoring');
if (monitoring && !process.env.GRAFANA_ADMIN_PASSWORD) {
  try { process.loadEnvFile('.env'); } catch { /* missing configuration reported below */ }
  if (!process.env.GRAFANA_ADMIN_PASSWORD) throw new Error('GRAFANA_ADMIN_PASSWORD_REQUIRED');
}
const env = {
  ...process.env,
  ...(monitoring ? { OTEL_EXPORTER_OTLP_ENDPOINT: 'http://collector:4318' } : {}),
};
const result = spawnSync(
  'docker',
  [
    'compose',
    '--env-file',
    '.env',
    '-f',
    'infra/compose.dev.yml',
    ...(monitoring ? ['--profile', 'monitoring'] : []),
    ...(action === 'up' ? ['up', '--build', '-d'] : ['stop']),
  ],
  { stdio: 'inherit', env },
);
process.exit(result.status ?? 1);
