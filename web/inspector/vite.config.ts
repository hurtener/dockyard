import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// web/inspector is a Svelte application. `vite build` emits a static dist/
// tree; the Go inspector backend (internal/inspector) embeds and serves it.
// The build output is relative-path so it serves correctly from the inspector's
// localhost listener at any port.
export default defineConfig({
  plugins: [svelte()],
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
});
