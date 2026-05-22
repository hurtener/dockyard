// Plain-Svelte configuration — no SvelteKit (Dockyard decision D-006).
// web/inspector is a plain Svelte 5 + Vite application: it builds to dist/ and
// is embedded into the Go inspector backend (internal/inspector). It composes
// the @dockyard/ui design system and the @dockyard/bridge ui/ protocol; nothing
// here depends on a SvelteKit runtime.
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default {
  preprocess: vitePreprocess(),
};
