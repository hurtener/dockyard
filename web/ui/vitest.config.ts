import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// web/ui ships Svelte components; component tests render them with
// @testing-library/svelte against jsdom, so the Svelte plugin compiles .svelte
// files for the test run.
export default defineConfig({
  plugins: [svelte({ hot: false })],
  // Resolve the browser build of Svelte: component tests mount() into jsdom,
  // and the server export of Svelte has no mount().
  resolve: {
    conditions: ['browser'],
  },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.test.ts'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.ts', 'src/**/*.svelte'],
      exclude: ['src/**/*.test.ts', 'src/**/__tests__/**', 'src/index.ts'],
      thresholds: {
        lines: 70,
        functions: 70,
        statements: 70,
        branches: 70,
      },
    },
  },
});
