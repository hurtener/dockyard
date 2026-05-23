/**
 * Vite ambient module shims for the inspector's static asset imports.
 *
 * Vite resolves `import logo from './assets/logo.png'` to a hashed URL at
 * build time; svelte-check / tsc need a module declaration so the import
 * type-checks. We declare only the image MIME types the inspector actually
 * imports — keep the surface minimal.
 */
declare module '*.png' {
  const src: string;
  export default src;
}

declare module '*.svg' {
  const src: string;
  export default src;
}
