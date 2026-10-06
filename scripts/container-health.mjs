import { statSync } from 'node:fs';
const target = process.argv[2];
try {
  if (target === '--heartbeat') {
    if (Date.now() - statSync('/tmp/reviewpilot-heartbeat').mtimeMs > 60000) process.exit(1);
  } else {
    const response = await fetch(target, { signal: AbortSignal.timeout(3000), redirect: 'manual' });
    if (!response.ok) process.exit(1);
  }
} catch {
  process.exit(1);
}
