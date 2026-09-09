import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { readFileSync, readdirSync } from 'node:fs'

// Keep PDF fonts/CMaps local. No CDN access or relaxed frame/script CSP needed.
function officePDFAssets() {
  let building = false
  const assets = new Map<string, Buffer>()
  for (const directory of ['cmaps', 'standard_fonts']) {
    const root = new URL(`./node_modules/pdfjs-dist/${directory}/`, import.meta.url)
    for (const name of readdirSync(root)) {
      if (name.endsWith('.bcmap') || name.endsWith('.pfb') || name.endsWith('.ttf'))
        assets.set(`pdfjs/${directory}/${name}`, readFileSync(new URL(name, root)))
    }
  }
  return {
    name: 'office-pdf-assets',
    configResolved(config: import('vite').ResolvedConfig) { building = config.command === 'build' },
    buildStart(this: { emitFile: (asset: { type: 'asset'; fileName: string; source: Buffer }) => void }) {
      if (!building) return
      for (const [fileName, source] of assets) this.emitFile({ type: 'asset', fileName, source })
    },
    configureServer(server: import('vite').ViteDevServer) {
      server.middlewares.use((request, response, next) => {
        const data = assets.get((request.url || '').split('?')[0].replace(/^\//, ''))
        if (!data) return next()
        response.setHeader('Content-Type', 'application/octet-stream')
        response.end(data)
      })
    },
  }
}

export default defineConfig({
  base: '/',
  plugins: [react(), officePDFAssets()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: { main: 'index.html', diagram: 'diagram-worker.html' },
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
