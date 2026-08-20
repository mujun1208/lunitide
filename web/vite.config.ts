import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  base: '/',
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: true },
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
