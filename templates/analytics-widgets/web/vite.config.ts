import { defineConfig, type Plugin } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { viteSingleFile } from 'vite-plugin-singlefile';

// The Dockyard App build is a single-file HTML bundle by default (RFC §7.4):
// zero external origins, so the deny-by-default CSP just works. The output
// HTML is embedded into the Go binary by `//go:embed all:dist` (RFC §14)
// and served as a `ui://` MCP resource.
//
// IMPORTANT — the bundle ships as a CLASSIC iife script, not a module. The
// inspector renders the App in a sandboxed iframe with `sandbox="allow-
// scripts"` (no `allow-same-origin`) — RFC §7.4's deny-by-default posture.
// Browsers refuse to execute `type="module"` scripts in an opaque-origin
// (sandboxed) document, so a module-format bundle would never run; only the
// classic `<script>` form executes. `format: 'iife'` + `inlineDynamicImports`
// give us a single, self-contained classic script, and `stripModuleType`
// (below) rewrites every `<script type="module">` Vite emits into the HTML
// to a plain `<script>` tag so the browser actually runs it inside the
// sandboxed iframe.

/**
 * stripModuleType rewrites `<script type="module" ...>` to `<script ...>` in
 * every generated HTML file. The vite-plugin-singlefile inlines the IIFE
 * bundle, but Vite's HTML transform still tags the inlined `<script>` with
 * `type="module"`. That tag bars execution inside a sandboxed iframe
 * (browser policy: opaque origins don't run module scripts), so we strip it
 * post-build. The bundle itself is already classic — see rollup output below.
 */
function stripModuleType(): Plugin {
  return {
    name: 'dockyard-strip-module-type',
    enforce: 'post',
    generateBundle(_options, bundle) {
      for (const fileName of Object.keys(bundle)) {
        const f = bundle[fileName];
        if (f.type !== 'asset' || !fileName.endsWith('.html')) continue;
        const src = typeof f.source === 'string'
          ? f.source
          : new TextDecoder().decode(f.source as Uint8Array);
        // Drop the type="module" attribute (with optional whitespace and
        // surrounding `crossorigin`/`async`). The bundle becomes a classic
        // script that runs in a sandboxed iframe.
        // Drop the type="module" attribute. The bundle itself is a classic
        // iife (see rollup output below); the mount in main.ts already
        // waits for DOMContentLoaded so it works whether the script lives
        // in <head> or <body>.
        const rewritten = src.replace(
          /<script([^>]*?)\stype="module"([^>]*)>/g,
          '<script$1$2>',
        );
        f.source = rewritten;
      }
    },
  };
}

export default defineConfig({
  plugins: [svelte(), viteSingleFile(), stripModuleType()],
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsInlineLimit: 100_000_000,
    cssCodeSplit: false,
    // Modern-syntax target so iife output stays compact; the App runs in the
    // user's host browser (current Chromium / Safari / Firefox).
    target: 'es2020',
    rollupOptions: {
      output: {
        format: 'iife',
        inlineDynamicImports: true,
      },
    },
  },
});
