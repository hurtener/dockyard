/**
 * main.ts — the inspector frontend entry point.
 *
 * Mounts the inspector `App` into the page. The Go inspector backend
 * (internal/inspector) serves this built bundle from its localhost listener;
 * the App talks back to the backend's read-only HTTP API at the same origin.
 */
import { mount } from 'svelte';
import 'dockyard-ui/tokens.css';
import App from './App.svelte';

const target = document.getElementById('app');
if (!target) {
  throw new Error('inspector: #app mount target missing');
}

mount(App, { target });
