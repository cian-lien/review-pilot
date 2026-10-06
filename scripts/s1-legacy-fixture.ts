import { mkdir, writeFile } from 'node:fs/promises';
import { sealToken } from '@reviewpilot/github';

const directory = new URL('../apps/api/internal/auth/testdata/', import.meta.url);
await mkdir(directory, { recursive: true });
const secret = Buffer.alloc(32, 7).toString('base64');
await writeFile(
  new URL('legacy-session.json', directory),
  JSON.stringify(
    {
      description: 'TEST ONLY: created by the legacy TypeScript AES-256-GCM implementation',
      secret,
      sealed: sealToken('test-token', secret),
      expectedToken: 'test-token',
    },
    null,
    2,
  ) + '\n',
  { flag: 'wx', mode: 0o600 },
);
