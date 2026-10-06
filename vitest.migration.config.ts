import { defineConfig } from 'vitest/config';
export default defineConfig({
  test: {
    include: ['tests/integration/s1-migration.test.ts'],
    fileParallelism: false,
    testTimeout: 120000,
    hookTimeout: 120000,
  },
});
