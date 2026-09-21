// Service worker ScheduleFU.
//
// Две разные стратегии, потому что у файлов разная природа:
//
//   оболочка (страница, скрипт, стили) — «сначала кэш»: она меняется редко,
//   и держать её в кэше значит открываться мгновенно и работать в здании,
//   где связи нет;
//
//   данные расписания — «сначала сеть, кэш как запас»: показывать вчерашнюю
//   занятость аудитории хуже, чем честно сказать, что данные старые.
//
// Версия в имени кэша обязательна: при выкладке новой версии старые файлы
// нужно выбросить целиком, иначе половина приложения останется прежней.

var VERSION = 'v1';
var SHELL_CACHE = 'schedulefu-shell-' + VERSION;
var DATA_CACHE = 'schedulefu-data-' + VERSION;

var SHELL = [
  './',
  './index.html',
  './app.js',
  './style.css',
  './manifest.webmanifest',
  './icon-192.png',
  './icon-512.png'
];

self.addEventListener('install', function (event) {
  event.waitUntil(
    caches.open(SHELL_CACHE).then(function (cache) {
      return cache.addAll(SHELL);
    }).then(function () {
      // Новая версия оболочки должна начать работать сразу, иначе
      // обновление увидят только те, кто закроет все вкладки.
      return self.skipWaiting();
    })
  );
});

self.addEventListener('activate', function (event) {
  event.waitUntil(
    caches.keys().then(function (names) {
      return Promise.all(names.map(function (name) {
        if (name !== SHELL_CACHE && name !== DATA_CACHE) {
          return caches.delete(name);
        }
      }));
    }).then(function () {
      return self.clients.claim();
    })
  );
});

self.addEventListener('fetch', function (event) {
  var request = event.request;
  if (request.method !== 'GET') return;

  var url = new URL(request.url);
  // Чужие адреса (шрифты) не перехватываем: браузер кэширует их сам.
  if (url.origin !== self.location.origin) return;

  if (url.pathname.indexOf('/data/') !== -1) {
    event.respondWith(networkFirst(request));
    return;
  }
  event.respondWith(cacheFirst(request));
});

function cacheFirst(request) {
  return caches.match(request).then(function (hit) {
    if (hit) {
      // Обновляем фоном: следующий запуск получит свежую оболочку,
      // а этот не ждёт сети.
      fetch(request).then(function (response) {
        if (response && response.ok) {
          caches.open(SHELL_CACHE).then(function (c) { c.put(request, response); });
        }
      }).catch(function () { /* офлайн — не беда, отдали из кэша */ });
      return hit;
    }
    return fetch(request);
  });
}

function networkFirst(request) {
  return fetch(request).then(function (response) {
    if (response && response.ok) {
      var copy = response.clone();
      caches.open(DATA_CACHE).then(function (c) { c.put(request, copy); });
    }
    return response;
  }).catch(function () {
    return caches.match(request).then(function (hit) {
      if (hit) return hit;
      // Ни сети, ни кэша: пусть приложение покажет свою ошибку, а не
      // пустую страницу браузера.
      return new Response(JSON.stringify({ error: 'offline' }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' }
      });
    });
  });
}

// Уведомление приходит от сервера через Web Push. На статической версии
// сервера нет, поэтому обработчик просто готов к нему: когда появится
// серверная часть, ничего в приложении менять не придётся.
self.addEventListener('push', function (event) {
  var payload = { title: 'Расписание изменилось', body: '', url: './#/schedule' };
  if (event.data) {
    try { payload = Object.assign(payload, event.data.json()); }
    catch (e) { payload.body = event.data.text(); }
  }
  event.waitUntil(self.registration.showNotification(payload.title, {
    body: payload.body,
    icon: './icon-192.png',
    badge: './icon-192.png',
    data: { url: payload.url },
    tag: 'schedulefu-change'
  }));
});

self.addEventListener('notificationclick', function (event) {
  event.notification.close();
  var target = (event.notification.data && event.notification.data.url) || './';
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (list) {
      for (var i = 0; i < list.length; i++) {
        if ('focus' in list[i]) return list[i].focus();
      }
      return self.clients.openWindow(target);
    })
  );
});
