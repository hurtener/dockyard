// Plain-Svelte configuration — no SvelteKit (Dockyard decision D-006).
// web/ui is a framework-agnostic component library: it ships plain Svelte 5
// components that the inspector, the template App UIs, and the docs site
// import directly. Nothing here depends on a SvelteKit runtime.
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default {
  preprocess: vitePreprocess(),
};
