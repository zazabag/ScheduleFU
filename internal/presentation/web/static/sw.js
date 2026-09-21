// Service worker серверной версии.
//
// В отличие от версии для GitHub Pages здесь он нужен только ради
// уведомлений: страницы приходят с сервера, кэшировать оболочку незачем —
// иначе пришлось бы следить за тем, чтобы кэш не отставал от шаблонов.

self.addEventListener('install', function () { self.skipWaiting(); });
self.addEventListener('activate', function (event) {
  event.waitUntil(self.clients.claim());
});

self.addEventListener('push', function (event) {
  var payload = { title: 'Расписание изменилось', body: '', url: '/schedule' };
  if (event.data) {
    try { payload = Object.assign(payload, event.data.json()); }
    catch (e) { payload.body = event.data.text(); }
  }
  event.waitUntil(self.registration.showNotification(payload.title, {
    body: payload.body,
    icon: '/static/icon-192.png',
    badge: '/static/icon-192.png',
    data: { url: payload.url },
    tag: 'schedulefu-change'
  }));
});

self.addEventListener('notificationclick', function (event) {
  event.notification.close();
  var target = (event.notification.data && event.notification.data.url) || '/';
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(function (list) {
      for (var i = 0; i < list.length; i++) {
        if ('focus' in list[i]) return list[i].focus();
      }
      return self.clients.openWindow(target);
    })
  );
});
