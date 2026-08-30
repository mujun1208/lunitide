import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  base: '/',
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: 'index.html',
    },
    // Assets under the inline limit become data: URLs, which is a saving for
    // an icon and a defect for an AudioWorklet: the renderer's CSP allows
    // scripts from 'self' only, and addModule() on a data: URL is refused at
    // runtime. Nothing catches that — the build succeeds, the types check,
    // and voice capture fails on the user's machine. Keep the worklet a file.
    assetsInlineLimit: (filePath: string) => (filePath.endsWith('Worklet.js') ? false : undefined),
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    // Guard against orphaned Cursor/CI runs burning CPU for hours when a
    // fake-timer + waitFor combo stalls; `vitest run` should always finish.
    testTimeout: 30_000,
    hookTimeout: 30_000,
    teardownTimeout: 10_000,
    pool: 'forks',
    maxWorkers: 4,
    fileParallelism: true,
    passWithNoTests: false,
  }
})
