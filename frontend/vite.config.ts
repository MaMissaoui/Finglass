import { writeFileSync } from 'node:fs';
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import wails from '@wailsio/runtime/plugins/vite';

// The Go embed pattern needs at least one file in dist before the first
// frontend build, so keep the committed placeholder after a clean build.
const keepDistPlaceholder = {
  name: 'keep-dist-placeholder',
  closeBundle() {
    writeFileSync('dist/.keep', '');
  },
};

export default defineConfig({
  plugins: [svelte(), wails('./bindings'), keepDistPlaceholder],
  server: {
    host: '127.0.0.1',
    port: 9245,
    strictPort: true,
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  preview: {
    host: '127.0.0.1',
    port: 4173,
    strictPort: true,
  },
});
