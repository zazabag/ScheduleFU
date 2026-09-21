// Живые мелочи поверх серверных страниц. Без скрипта всё работает:
// счётчик показывает минуты с сервера, дни листаются ссылками.
(function () {
  'use strict';

  // ─── обратный отсчёт и кольцо прогресса ──────────────────────────────────
  function minutes(hhmm) {
    var p = hhmm.split(':');
    return parseInt(p[0], 10) * 60 + parseInt(p[1], 10);
  }
  function two(n) { return (n < 10 ? '0' : '') + n; }

  document.querySelectorAll('.hero-timer').forEach(function (timer) {
    var state = timer.getAttribute('data-state');
    var begins = timer.getAttribute('data-begins');
    var ends = timer.getAttribute('data-ends');
    var next = timer.getAttribute('data-next');
    var out = timer.querySelector('.hero-countdown');
    var bar = timer.querySelector('.ring-bar');
    var progress = timer.closest('.hero, .now-card');
    var slider = progress ? progress.querySelector('.hero-progress') : null;

    // Что считаем: до конца текущей пары, либо до начала следующей.
    var from, to;
    if (state === 'now' && begins && ends) { from = begins; to = ends; }
    else if ((state === 'between' || state === 'before') && begins) { from = null; to = begins; }
    else return;

    // Точка отсчёта — время сервера на момент загрузки (data-now): часы
    // телефона могут врать, а в разработке время подменяется параметром.
    var loadedAt = Date.now();
    var serverNow = timer.getAttribute('data-now');
    var baseSec = serverNow ? minutes(serverNow) * 60 + new Date().getSeconds() : null;
    function tick() {
      var now = new Date();
      var nowSec = baseSec !== null
        ? baseSec + Math.floor((Date.now() - loadedAt) / 1000)
        : now.getHours() * 3600 + now.getMinutes() * 60 + now.getSeconds();
      var toSec = minutes(to) * 60;
      var left = Math.max(0, toSec - nowSec);
      var text = Math.floor(left / 60) + ':' + two(left % 60);
      if (out) out.textContent = text;
      var leftLabel = progress ? progress.querySelector('.hero-progress-left') : null;
      if (leftLabel) leftLabel.textContent = '−' + text;
      var pct;
      if (from) {
        var total = (minutes(to) - minutes(from)) * 60;
        pct = total > 0 ? Math.min(100, Math.max(0, 100 - left * 100 / total)) : 100;
      } else {
        pct = 0;
      }
      if (bar) bar.style.setProperty('--progress', pct);
      if (slider) slider.style.setProperty('--progress', pct);
      timer.style.setProperty('--progress', pct);
      if (left === 0) { clearInterval(handle); setTimeout(function () { location.reload(); }, 1500); }
    }
    tick();
    var handle = setInterval(tick, 1000);
  });

  // ─── листание дней свайпом ───────────────────────────────────────────────
  var days = document.querySelector('.days');
  var screen = document.getElementById('screen');
  if (days && screen) {
    var x0 = null, y0 = null;
    screen.addEventListener('touchstart', function (e) {
      if (e.touches.length !== 1) return;
      x0 = e.touches[0].clientX; y0 = e.touches[0].clientY;
    }, { passive: true });
    screen.addEventListener('touchend', function (e) {
      if (x0 === null) return;
      var dx = e.changedTouches[0].clientX - x0, dy = e.changedTouches[0].clientY - y0;
      x0 = null;
      if (Math.abs(dx) < 70 || Math.abs(dy) > Math.abs(dx)) return;
      var href = days.getAttribute(dx < 0 ? 'data-next' : 'data-prev');
      if (href) location.href = href;
    }, { passive: true });
  }

  // ─── настройки: оформление и тема применяются сразу ──────────────────────
  // Без скрипта работает кнопка «Применить»; со скриптом она не нужна —
  // выбор отправляется сам, страница перерисовывается в новом виде.
  var lookForm = document.getElementById('look-form');
  if (lookForm) {
    var applyBtn = lookForm.querySelector('[data-autosubmit-hide]');
    if (applyBtn) applyBtn.hidden = true;
    lookForm.addEventListener('change', function (e) {
      if (e.target && e.target.name && (e.target.name === 'skin' || e.target.name === 'theme')) lookForm.submit();
    });
  }

  // ─── копирование адреса календаря ────────────────────────────────────────
  document.querySelectorAll('[data-copy]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var text = btn.getAttribute('data-copy');
      var done = function () { var t = btn.textContent; btn.textContent = 'Скопировано'; setTimeout(function () { btn.textContent = t; }, 1500); };
      if (navigator.clipboard) navigator.clipboard.writeText(text).then(done);
    });
  });

  // ─── без масштабирования: ни щипком, ни двойным тапом ────────────────────
  // Safari игнорирует user-scalable=no в браузере (уважает только в
  // приложении), поэтому жесты гасим сами.
  document.addEventListener('gesturestart', function (e) { e.preventDefault(); }, { passive: false });
  document.addEventListener('gesturechange', function (e) { e.preventDefault(); }, { passive: false });
  document.addEventListener('touchmove', function (e) { if (e.touches.length > 1 || (e.scale && e.scale !== 1)) e.preventDefault(); }, { passive: false });
  var lastTap = 0;
  document.addEventListener('touchend', function (e) {
    var now = Date.now();
    if (now - lastTap < 300 && !e.target.closest('input, textarea, button, a, summary, label')) e.preventDefault();
    lastTap = now;
  }, { passive: false });

  // ─── потяни вниз — обнови ────────────────────────────────────────────────
  // Прокрутка внутри экрана, родного жеста браузера нет — рисуем свой.
  var ptr = document.getElementById('ptr');
  if (screen && ptr) {
    var startY = null, pulling = false;
    screen.addEventListener('touchstart', function (e) {
      if (screen.scrollTop === 0 && e.touches.length === 1) { startY = e.touches[0].clientY; pulling = true; }
    }, { passive: true });
    screen.addEventListener('touchmove', function (e) {
      if (!pulling || startY === null) return;
      var dy = e.touches[0].clientY - startY;
      if (dy <= 0 || screen.scrollTop > 0) { ptr.style.height = '0px'; ptr.classList.remove('armed'); return; }
      var h = Math.min(72, dy * 0.5);
      ptr.style.height = h + 'px';
      ptr.classList.toggle('armed', h >= 56);
      ptr.textContent = h >= 56 ? 'отпусти — обновится' : 'потяни, чтобы обновить';
    }, { passive: true });
    screen.addEventListener('touchend', function () {
      if (!pulling) return;
      pulling = false; startY = null;
      if (ptr.classList.contains('armed')) {
        ptr.textContent = 'обновляем…'; ptr.classList.add('busy');
        location.reload();
      } else {
        ptr.style.height = '0px';
      }
    }, { passive: true });
  }

  // ─── онбординг: три шага, установка на рабочий стол ───────────────────
  var onb = document.getElementById('onb');
  var installPrompt = null;
  window.addEventListener('beforeinstallprompt', function (e) {
    e.preventDefault();
    installPrompt = e;
    var btn = onb && onb.querySelector('.onb-install-btn');
    if (btn) btn.textContent = 'Установить';
  });
  if (onb) {
    var steps = onb.querySelectorAll('.onb-step');
    var standalone = window.matchMedia && window.matchMedia('(display-mode: standalone)').matches || window.navigator.standalone === true;
    var installStep = onb.querySelector('[data-step="install"]');
    var howto = onb.querySelector('.onb-howto');
    var ua = navigator.userAgent;
    var ios = /iPhone|iPad|iPod/.test(ua);
    var android = /Android/.test(ua);

    function recount() {
      var done = 0;
      steps.forEach(function (s) { if (s.classList.contains('done')) done++; });
      var out = onb.querySelector('.onb-done');
      if (out) out.textContent = done;
      var kicker = onb.querySelector('.onb-kicker');
      if (kicker) kicker.setAttribute('data-left', steps.length - done);
      onb.classList.toggle('all-done', done === steps.length);
      if (done === steps.length) {
        // Всё сделано — блок больше не нужен нигде.
        try { localStorage.setItem('onb-done', '1'); } catch (e) {}
        onb.hidden = true;
      }
    }
    window.ScheduleFUOnboard = { recount: recount };

    if (standalone && installStep) {
      installStep.classList.add('done');
      var t = installStep.querySelector('.onb-install-text');
      if (t) t.textContent = 'Открыто как приложение';
      var ib = installStep.querySelector('.onb-install-btn');
      if (ib) ib.textContent = 'Готово';
    }
    var installBtn = onb.querySelector('.onb-install-btn');
    if (installBtn) {
      installBtn.addEventListener('click', function () {
        if (installStep.classList.contains('done')) return;
        if (installPrompt) {
          installPrompt.prompt();
          installPrompt.userChoice.then(function (choice) {
            if (choice && choice.outcome === 'accepted') { installStep.classList.add('done'); recount(); }
          });
          return;
        }
        // Без системного диалога — показываем, как это делается руками.
        if (howto) {
          howto.hidden = !howto.hidden;
          howto.querySelector('.onb-howto-ios').hidden = !ios;
          howto.querySelector('.onb-howto-android').hidden = !android;
          howto.querySelector('.onb-howto-desktop').hidden = ios || android;
        }
      });
    }
    // Сворачивание — на одну строку и только до перезагрузки: напоминание
    // обязано вернуться, пока шаги не сделаны.
    var fold = onb.querySelector('.onb-fold');
    if (fold) {
      fold.addEventListener('click', function () {
        onb.classList.toggle('folded');
        try { sessionStorage.setItem('onb-folded', onb.classList.contains('folded') ? '1' : ''); } catch (e) {}
      });
      try { if (sessionStorage.getItem('onb-folded') === '1') onb.classList.add('folded'); } catch (e) {}
    }
    onb.addEventListener('click', function (e) {
      if (onb.classList.contains('folded') && !e.target.closest('.onb-fold')) {
        onb.classList.remove('folded');
        try { sessionStorage.setItem('onb-folded', ''); } catch (e2) {}
      }
    });
    try { if (localStorage.getItem('onb-done') === '1' && onb.getAttribute('data-pinned') === '1') onb.hidden = true; } catch (e) {}
    recount();
  }
})();
