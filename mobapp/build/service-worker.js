/* SKUD service worker — автономный, без внешних зависимостей (нет importScripts с CDN).
   Обязателен для установки PWA (standalone-ярлык без иконки браузера)
   и для офлайн-работы фронтенда. */

const CACHE_NAME = 'skud-cache-v3';
const CORE_ASSETS = [
  '/',
  '/index.html',
  '/manifest.json',
  '/logo192.png',
  '/logo512.png',
  '/favicon.ico',
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME)
      .then((cache) => cache.addAll(CORE_ASSETS))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(
        keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))
      ))
      .then(() => self.clients.claim())
  );
});

// Кэшируем только успешные ответы своего origin
const putInCache = (request, response) => {
  if (response && response.status === 200 && response.type === 'basic') {
    const clone = response.clone();
    caches.open(CACHE_NAME).then((cache) => cache.put(request, clone));
  }
  return response;
};

self.addEventListener('fetch', (event) => {
  const request = event.request;
  const url = new URL(request.url);

  // Только свой origin и только GET
  if (url.origin !== self.location.origin) return;
  if (request.method !== 'GET') return;

  // API-запросы никогда не кэшируем — только сеть (иначе QR будет «залипать»)
  if (url.pathname.startsWith('/api/')) return;

  // Навигация и HTML — network-first: обновления приложения доходят сразу,
  // кэш используется только офлайн. Иначе телефон навсегда работает на старой сборке.
  const isHTML =
    request.mode === 'navigate' ||
    url.pathname === '/' ||
    url.pathname.endsWith('.html');

  if (isHTML) {
    event.respondWith(
      fetch(request)
        .then((response) => putInCache(request, response))
        .catch(() =>
          caches.match('/index.html').then(
            (cached) =>
              cached ||
              new Response('Нет сети и нет кэша', {
                status: 503,
                headers: { 'Content-Type': 'text/plain; charset=utf-8' },
              })
          )
        )
    );
    return;
  }

  // Статика с хэшем в имени неизменяема — cache-first (быстрый старт + офлайн)
  event.respondWith(
    caches.match(request, { ignoreSearch: true }).then(
      (cached) =>
        cached || fetch(request).then((response) => putInCache(request, response))
    )
  );
});