import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// web/inspector is a Svelte application. `vite build` emits a static dist/
// tree; the Go inspector backend (internal/inspector) embeds and serves it.
// The build output is relative-path so it serves correctly from the inspector's
// localhost listener at any port.
export default defineConfig({
  plugins: [svelte()],
  base: './',
  // `dockyard-bridge` is a `file:`-linked source package (D-172). Its `./spec`
  // subpath imports `zod` + `@modelcontextprotocol/sdk`, which the inspector
  // provides as its own devDeps. Dedupe them from the inspector root so they
  // resolve from web/inspector/node_modules — Vite otherwise resolves the
  // symlinked bridge's imports from web/bridge/node_modules, which the
  // `make build` job does not install (it `npm ci`s web/inspector only).
  resolve: {
    dedupe: ['zod', '@modelcontextprotocol/sdk', 'svelte'],
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
});
