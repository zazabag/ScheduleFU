// Подписка на уведомления об изменениях расписания.
//
// Скрипт необязательный: страницы работают без него полностью, он только
// добавляет кнопку. Поэтому всё внутри защищено проверками — при любой
// неподдерживаемой мелочи кнопка просто не появляется.

(function () {
  'use strict';

  var box = document.getElementById('notify-box');
  if (!box) return;

  var subjectKey = box.getAttribute('data-subject');
  if (!subjectKey) return;

  var button = box.querySelector('button');
  var label = box.querySelector('.notify-text');

  function say(text, disabled) {
    if (label) label.textContent = text;
    if (button) button.disabled = !!disabled;
  }

  // Уведомления требуют защищённого соединения: на http браузер не даст
  // ни разрешения, ни service worker.
  if (!window.isSecureContext) {
    say('Уведомления работают только по защищённому соединению', true);
    return;
  }
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    say('Этот браузер не умеет уведомления', true);
    return;
  }

  // Ключ VAPID приходит в виде base64url, а браузеру нужен массив байтов.
  function decodeKey(base64) {
    var padded = (base64 + '='.repeat((4 - base64.length % 4) % 4))
      .replace(/-/g, '+').replace(/_/g, '/');
    var raw = atob(padded);
    var out = new Uint8Array(raw.length);
    for (var i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
    return out;
  }

  function post(path, body) {
    return fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }).then(function (r) {
      if (!r.ok) return r.json().then(function (e) { throw new Error(e.error || r.status); });
      return r.json();
    });
  }

  var state = { publicKey: '', registration: null, subscription: null };

  function refresh() {
    if (!state.subscription) {
      say('Сообщать об изменениях расписания');
      if (button) button.textContent = 'Включить';
      return;
    }
    say('Уведомления включены');
    if (button) button.textContent = 'Выключить';
  }

  function enable() {
    say('Спрашиваем разрешение…', true);
    return Notification.requestPermission().then(function (permission) {
      if (permission !== 'granted') {
        // Отказ — это ответ, а не ошибка: повторно спрашивать браузер всё
        // равно не даст, поэтому просто говорим, где это включается.
        say('Разрешение не выдано. Включить можно в настройках сайта', true);
        return null;
      }
      return state.registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: decodeKey(state.publicKey)
      });
    }).then(function (sub) {
      if (!sub) return;
      var json = sub.toJSON();
      return post('/api/v1/push/subscribe', {
        subject_key: subjectKey,
        endpoint: json.endpoint,
        keys: json.keys
      }).then(function () {
        state.subscription = sub;
        refresh();
      });
    }).catch(function (e) {
      say('Не получилось включить: ' + (e && e.message ? e.message : 'ошибка'), false);
    });
  }

  function disable() {
    say('Выключаем…', true);
    var endpoint = state.subscription.endpoint;
    return post('/api/v1/push/unsubscribe', { subject_key: subjectKey, endpoint: endpoint })
      .then(function () {
        // Подписку в браузере снимаем только после того, как сервер о ней
        // забыл: иначе при сбое остались бы письма в никуда.
        return state.subscription.unsubscribe();
      })
      .then(function () {
        state.subscription = null;
        refresh();
      })
      .catch(function (e) {
        say('Не получилось выключить: ' + (e && e.message ? e.message : 'ошибка'), false);
      });
  }

  fetch('/api/v1/push/key')
    .then(function (r) { return r.json(); })
    .then(function (cfg) {
      if (!cfg.enabled) {
        // Сервер собран без ключей — кнопки быть не должно.
        box.hidden = true;
        return;
      }
      state.publicKey = cfg.public_key;
      return navigator.serviceWorker.register('/static/sw.js').then(function (reg) {
        state.registration = reg;
        return reg.pushManager.getSubscription();
      }).then(function (sub) {
        state.subscription = sub;
        box.hidden = false;
        refresh();
        if (button) {
          button.addEventListener('click', function () {
            if (state.subscription) disable(); else enable();
          });
        }
      });
    })
    .catch(function () {
      box.hidden = true;
    });
})();
