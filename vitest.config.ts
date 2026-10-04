import { defineConfig } from 'vitest/config';
export default defineConfig({
  test: {
    include: ['packages/*/tests/**/*.test.ts', 'apps/*/tests/**/*.test.ts'],
    exclude: ['**/node_modules/**', '**/*.integration.test.ts'],
    testTimeout: 10000,
  },
});
