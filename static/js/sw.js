const CACHE_VERSION = 'bgtags-v2';
const STATIC_CACHE = `${CACHE_VERSION}-static`;
const PAGES_CACHE = `${CACHE_VERSION}-pages`;
const RULES_CACHE = `${CACHE_VERSION}-rules`;

const PRECACHE_URLS = [
  '/',
  '/manifest.webmanifest',
  '/static/css/styles.css',
  '/static/js/htmx.min.js',
  '/static/img/bgtags-icon.png',
  '/static/img/bgtags-icon-192.png',
  '/static/favicon.png'
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(STATIC_CACHE).then((cache) => {
      return cache.addAll(PRECACHE_URLS).catch((err) => {
        console.warn('[SW] Pre-cache warning:', err);
      });
    }).then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => {
      return Promise.all(
        keys.filter((key) => !key.startsWith(CACHE_VERSION)).map((key) => caches.delete(key))
      );
    }).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const request = event.request;
  const url = new URL(request.url);

  // Only handle GET requests
  if (request.method !== 'GET') {
    return;
  }

  // Bypass cache for Auth & Admin endpoints
  if (url.pathname.startsWith('/auth/') || 
      url.pathname.startsWith('/login') || 
      url.pathname.startsWith('/logout') || 
      url.pathname.startsWith('/admin')) {
    return;
  }

  // 1. PDF Rulebooks (/rules/* or /games/{id}/rules)
  if (url.pathname.startsWith('/rules/') || (url.pathname.startsWith('/games/') && url.pathname.endsWith('/rules'))) {
    event.respondWith(
      fetch(request).then((networkResponse) => {
        if (networkResponse && networkResponse.status === 200) {
          const cloned = networkResponse.clone();
          caches.open(RULES_CACHE).then((cache) => cache.put(request, cloned));
        }
        return networkResponse;
      }).catch(() => {
        return caches.match(request);
      })
    );
    return;
  }

  // 2. Static Assets (CSS, JS, Static images)
  if (url.pathname.startsWith('/static/') || url.pathname.startsWith('/img/')) {
    event.respondWith(
      caches.match(request).then((cachedResponse) => {
        if (cachedResponse) {
          // Stale-while-revalidate in background
          fetch(request).then((networkResponse) => {
            if (networkResponse && networkResponse.status === 200) {
              caches.open(STATIC_CACHE).then((cache) => cache.put(request, networkResponse));
            }
          }).catch(() => {});
          return cachedResponse;
        }
        return fetch(request).then((networkResponse) => {
          if (networkResponse && networkResponse.status === 200) {
            const cloned = networkResponse.clone();
            caches.open(STATIC_CACHE).then((cache) => cache.put(request, cloned));
          }
          return networkResponse;
        });
      })
    );
    return;
  }

  // 3. HTML Pages (Catalog, Hubs, Stickers) - Network-First with Cache fallback
  if (request.mode === 'navigate' || request.headers.get('Accept')?.includes('text/html')) {
    event.respondWith(
      fetch(request).then((networkResponse) => {
        if (networkResponse && networkResponse.status === 200) {
          const cloned = networkResponse.clone();
          caches.open(PAGES_CACHE).then((cache) => cache.put(request, cloned));
        }
        return networkResponse;
      }).catch(async () => {
        const cached = await caches.match(request);
        if (cached) return cached;
        const catalogFallback = await caches.match('/');
        if (catalogFallback) return catalogFallback;
        return new Response('<h1>Offline</h1><p>You are currently offline and this page is not yet cached.</p>', {
          headers: { 'Content-Type': 'text/html' }
        });
      })
    );
    return;
  }

  // Fallback default fetch
  event.respondWith(
    caches.match(request).then((cached) => cached || fetch(request))
  );
});
