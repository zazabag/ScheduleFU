// Подписка на уведомления об изменениях расписания.
//
// Скрипт необязательный: страницы работают без него полностью, он только
// оживляет кнопки. Кнопок может быть несколько на странице — в онбординге
// и в настройках; все они про одну подписку и меняются вместе.

(function () {
  'use strict';

  var boxes = Array.prototype.slice.call(document.querySelectorAll('.push-box[data-subject], .onb-step[data-step="push"]'));
  if (!boxes.length) return;

  var onb = document.getElementById('onb');
  var subjectKey = (onb && onb.getAttribute('data-subject')) || (boxes[0].getAttribute('data-subject') || '');

  function label(box) { return box.querySelector('.notify-text, .onb-push-text'); }
  function button(box) { return box.querySelector('button'); }

  function say(text, disabled) {
    boxes.forEach(function (box) {
      var l = label(box), b = button(box);
      if (l) l.textContent = text;
      if (b) b.disabled = !!disabled;
    });
  }
  function markDone(done) {
    boxes.forEach(function (box) {
      if (box.classList.contains('onb-step')) box.classList.toggle('done', done);
    });
    if (window.ScheduleFUOnboard) window.ScheduleFUOnboard.recount();
  }

  if (!subjectKey) {
    // Группа не закреплена — шаг заперт, кнопки нет; ничего не трогаем.
    return;
  }

  // Уведомления требуют защищённого соединения: на http браузер не даст
  // ни разрешения, ни service worker.
  if (!window.isSecureContext) {
    say('Уведомления работают только по защищённому соединению', true);
    return;
  }
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    // iPhone до установки на экран «Домой» уведомлений не даёт вовсе.
    var ios = /iPhone|iPad|iPod/.test(navigator.userAgent);
    say(ios ? 'На iPhone — сначала шаг 3, потом уведомления из приложения' : 'Этот браузер не умеет уведомления', true);
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
      say('Одно сообщение, когда деканат поменяет расписание');
      boxes.forEach(function (box) { var b = button(box); if (b) b.textContent = box.classList.contains('onb-step') ? 'Разрешить' : 'Включить'; });
      markDone(false);
      return;
    }
    say('Уведомления включены');
    boxes.forEach(function (box) { var b = button(box); if (b) b.textContent = box.classList.contains('onb-step') ? 'Готово' : 'Выключить'; });
    markDone(true);
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
        morningBox(sub);
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

  // Подписка, заведённая до напоминаний о заданиях, не знает ключа
  // устройства. Раз в день тихо подтверждаем её с этого устройства —
  // разрешение заново не спрашивается, это та же подписка.
  function relink(sub) {
    if (!sub || !subjectKey) return;
    var today = new Date().toISOString().slice(0, 10);
    try { if (localStorage.getItem('push-linked') === today) return; } catch (e) {}
    var json = sub.toJSON();
    post('/api/v1/push/subscribe', { subject_key: subjectKey, endpoint: json.endpoint, keys: json.keys })
      .then(function () { try { localStorage.setItem('push-linked', today); } catch (e) {} })
      .catch(function () {});
  }

  // Утренняя сводка — по желанию, у той же подписки. Переключатель виден,
  // только когда уведомления включены: без подписки слать некуда.
  function morningBox(sub) {
    var box = document.getElementById('morning-box');
    var input = document.getElementById('morning');
    if (!box || !input) return;
    box.hidden = !sub;
    if (!sub) return;
    var endpoint = sub.toJSON().endpoint;
    post('/api/v1/push/morning', { subject_key: subjectKey, endpoint: endpoint })
      .then(function (r) { input.checked = !!r.on; })
      .catch(function () { box.hidden = true; });
    if (input.dataset.bound) return;
    input.dataset.bound = '1';
    input.addEventListener('change', function () {
      var want = input.checked;
      post('/api/v1/push/morning', { subject_key: subjectKey, endpoint: endpoint, on: want })
        .catch(function () { input.checked = !want; });
    });
  }

  fetch('/api/v1/push/key')
    .then(function (r) { return r.json(); })
    .then(function (cfg) {
      if (!cfg.enabled) {
        // Сервер собран без ключей — кнопки быть не должно.
        say('На этом сервере уведомления выключены', true);
        return;
      }
      state.publicKey = cfg.public_key;
      return navigator.serviceWorker.register('/static/sw.js').then(function (reg) {
        state.registration = reg;
        return reg.pushManager.getSubscription();
      }).then(function (sub) {
        state.subscription = sub;
        refresh();
        morningBox(sub);
        relink(sub);
        boxes.forEach(function (box) {
          var b = button(box);
          if (!b) return;
          b.addEventListener('click', function () {
            // В онбординге кнопка «Готово» ничего не делает: выключить — в настройках.
            if (state.subscription && box.classList.contains('onb-step')) return;
            if (state.subscription) disable(); else enable();
          });
        });
      });
    })
    .catch(function () {
      say('Уведомления сейчас недоступны', true);
    });
})();
