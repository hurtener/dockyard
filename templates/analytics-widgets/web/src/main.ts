import { mount } from 'svelte';
import App from './App.svelte';

// The App's Svelte component owns the bridge lifecycle: the bridge is
// constructed in App.svelte's <script>, `bridge.connect()` is kicked off
// from its onMount, and `bridge.close()` runs from onDestroy. This file's
// only job is to mount the App into the iframe's #app node after the
// document is ready.

function bootstrap(): void {
  const target = document.getElementById('app');
  if (!target) {
    throw new Error('analytics-widgets: #app mount node missing');
  }
  mount(App, { target });
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', bootstrap, { once: true });
} else {
  bootstrap();
}
