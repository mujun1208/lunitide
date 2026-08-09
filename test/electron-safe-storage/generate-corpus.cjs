const { app, safeStorage } = require('electron')
const fs = require('node:fs')
const path = require('node:path')

app.whenReady().then(() => {
  const output = process.env.LUNITIDE_ELECTRON_CORPUS
  const apiKey = process.env.LUNITIDE_ELECTRON_CORPUS_CANARY
  if (!output || !apiKey) throw new Error('corpus output and canary are required')
  if (!safeStorage.isEncryptionAvailable()) throw new Error('Electron safeStorage is unavailable')
  const origin = 'https://example.test'
  const protocol = 'openai'
  const encryptedApiKey = safeStorage.encryptString(JSON.stringify({ version: 1, apiKey, origin, protocol })).toString('base64')
  fs.writeFileSync(output, JSON.stringify({
    version: 2,
    providers: [{
      id: 'electron-safe-storage-e2e',
      name: 'Electron safeStorage E2E',
      protocol,
      baseUrl: `${origin}/v1`,
      models: ['model-a'],
      defaultModel: 'model-a',
      createdAt: '2025-01-01T00:00:00Z',
      updatedAt: '2025-01-02T00:00:00Z',
      encryptedApiKey
    }]
  }))
  fs.copyFileSync(path.join(app.getPath('userData'), 'Local State'), path.join(path.dirname(output), 'Local State'))
  app.quit()
}).catch(error => {
  console.error(error)
  app.exit(1)
})
