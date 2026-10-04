import { loadEnvFile } from 'node:process';

// CLI env-file flags are copied by Next dev into NODE_OPTIONS, where Node rejects them.
try {
  loadEnvFile(new URL('../../../.env', import.meta.url));
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}

await import('next/dist/bin/next');
