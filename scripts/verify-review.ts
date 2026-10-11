import { spawn } from 'node:child_process';
const affected = [
  'api',
  'web',
  'contracts',
  'observability',
  'repository',
  'model-gateway',
  'review-engine',
  'worker-runtime',
  'tooling',
].map((name) => '--filter=@reviewpilot/' + name);
const steps = [
  ['contracts:check', '--filter=@reviewpilot/contracts'],
  ['typecheck', 'lint', 'build', ...affected],
  [
    'test:unit',
    ...[
      'contracts',
      'repository',
      'model-gateway',
      'review-engine',
      'worker-runtime',
      'observability',
      'web',
    ].map((name) => '--filter=@reviewpilot/' + name),
  ],
  [
    'test:unit',
    '--filter=@reviewpilot/api',
    '--',
    './internal/config',
    './internal/retention',
    './internal/contracts',
    './internal/reviews',
    './internal/publisher',
    '-count=1',
  ],
  ...['context', 'budget', 'stages', 'results', 'publication', 'pipeline'].map((name) => [
    'test:review',
    '--filter=@reviewpilot/tooling',
    '--',
    '--case',
    name,
  ]),
  ...['message-recovery', 'cutover'].map((name) => [
    'test:migration',
    '--filter=@reviewpilot/tooling',
    '--',
    '--case',
    name,
  ]),
  ['infra:check', '--filter=@reviewpilot/tooling'],
];
// Start from a credential-free environment. Each scenario owns its database and remote fixtures.
const env = {
  PATH: process.env.PATH,
  HOME: process.env.HOME,
  LANG: process.env.LANG,
  TMPDIR: process.env.TMPDIR,
  REVIEW_TEST_ISOLATED: '1',
  NEXT_TELEMETRY_DISABLED: '1',
};
for (const [index, args] of steps.entries()) {
  console.log('S2_VERIFY_STEP ' + (index + 1) + '/' + steps.length + ' ' + args.join(' '));
  const child = spawn('pnpm', ['exec', 'turbo', 'run', ...args], { stdio: 'inherit', env });
  const status = await new Promise<number>((resolve, reject) => {
    child.once('error', reject);
    child.once('exit', (code, signal) => resolve(signal ? 1 : (code ?? 1)));
  });
  if (status) {
    console.error('S2_VERIFY_FAILED_AT_STEP_' + (index + 1));
    process.exit(status);
  }
}
console.log(
  'S2_LOCAL_SIMULATED_VERIFICATION_PASSED: 仅本地模拟通过；真实 GitHub、模型、账单、镜像运行与生产部署尚未验收。',
);
