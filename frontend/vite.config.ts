import { mkdirSync, writeFileSync } from 'node:fs';
import { basename, join } from 'node:path';
import { defineConfig, type Plugin, type ResolvedConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import wails from '@wailsio/runtime/plugins/vite';

// The Go embed pattern needs at least one file in dist before the first
// frontend build, so keep the committed placeholder after a clean build.
// Only the real build's dist gets it; the mock build goes to dist-mock.
function keepDistPlaceholder(): Plugin {
  let outDir = '';
  return {
    name: 'keep-dist-placeholder',
    apply: 'build',
    configResolved(config: ResolvedConfig) {
      outDir = join(config.root, config.build.outDir);
    },
    closeBundle() {
      if (basename(outDir) !== 'dist') return;
      mkdirSync(outDir, { recursive: true });
      writeFileSync(join(outDir, '.keep'), '');
    },
  };
}

export default defineConfig({
  plugins: [svelte(), wails('./bindings'), keepDistPlaceholder()],
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
