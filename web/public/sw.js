// Lunitide 移动伴侣 Service Worker：只做离线壳兜底。
// 在线优先（网关在线是产品常态）；navigate 请求失败时回退缓存壳，
// 让 PWA 图标在断网时也能打开并显示明确的离线提示，而不是浏览器报错。
// /bridge（WebSocket）与 /api/* 永不缓存。
const SHELL_CACHE = 'lunitide-shell-v1'
const SHELL_ASSETS = ['/', '/pair', '/manifest.webmanifest', '/brand/icon-192.png', '/brand/icon-512.png']

self.addEventListener('install', event => {
  event.waitUntil((async () => {
    const cache = await caches.open(SHELL_CACHE)
    await Promise.allSettled(SHELL_ASSETS.map(asset => cache.add(new Request(asset, { cache: 'reload' }))))
    await self.skipWaiting()
  })())
})

self.addEventListener('activate', event => {
  event.waitUntil((async () => {
    const names = await caches.keys()
    await Promise.all(names.filter(name => name !== SHELL_CACHE).map(name => caches.delete(name)))
    await self.clients.claim()
  })())
})

self.addEventListener('fetch', event => {
  const request = event.request
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return
  if (url.pathname === '/bridge' || url.pathname.startsWith('/api/')) return
  if (request.mode !== 'navigate' && !url.pathname.startsWith('/brand/')) return
  event.respondWith((async () => {
    try {
      const response = await fetch(request)
      if (response.ok && (request.mode === 'navigate' || url.pathname.startsWith('/brand/'))) {
        const cache = await caches.open(SHELL_CACHE)
        cache.put(request, response.clone())
      }
      return response
    } catch {
      const cache = await caches.open(SHELL_CACHE)
      const cached = await cache.match(request, { ignoreSearch: request.mode === 'navigate' })
      if (cached) return cached
      if (request.mode === 'navigate') {
        const shell = await cache.match('/', { ignoreSearch: true })
        if (shell) return shell
      }
      return new Response('offline', { status: 503, headers: { 'Content-Type': 'text/plain; charset=utf-8' } })
    }
  })())
})
