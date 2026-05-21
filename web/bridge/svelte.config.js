// Plain-Svelte configuration — no SvelteKit (Dockyard decision D-006).
// The bridge shell is a framework-agnostic library; it ships Svelte stores
// but is consumed by app authors as a plain Svelte/TypeScript dependency.
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default {
  preprocess: vitePreprocess(),
};
