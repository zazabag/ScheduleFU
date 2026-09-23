// Запись пары.
//
// Скрипт необязательный: без него в разделе работает всё, кроме записи с
// микрофона — файл, записанный чем угодно другим, отправляется обычной
// формой. Это не запасной путь, а основной для айфона: PWA на iOS засыпает
// вместе с экраном, и полтора часа она не пишет.
//
// Главное решение здесь — куски. Запись уезжает на сервер по ходу пары
// кусками по пятнадцать секунд, а не одним файлом в конце: браузер на
// телефоне закрывают, выгружают из памяти и теряют сеть, и разница между
// «потеряли минуту» и «потеряли лекцию» — вот эта.

(function () {
  'use strict';

  var form = document.getElementById('rec-form');
  var startBtn = document.getElementById('rec-start');
  if (!form || !startBtn) { pollRecording(); return; }

  var supported = !!(navigator.mediaDevices && navigator.mediaDevices.getUserMedia && window.MediaRecorder);
  if (!supported || !window.isSecureContext) {
    // Без микрофона кнопки просто нет: обещать запись и не суметь — хуже,
    // чем предложить сразу загрузить файл.
    pollRecording();
    return;
  }
  startBtn.hidden = false;

  // Айфон выключает микрофон свёрнутому приложению — и в PWA, и в Safari.
  // Обойти это из браузера нельзя, поэтому честно предупреждаем заранее.
  var isIOS = /iPad|iPhone|iPod/.test(navigator.userAgent) ||
    (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
  var iosHint = document.getElementById('rec-ios');
  if (isIOS && iosHint) iosHint.hidden = false;

  var stopBtn = document.getElementById('rec-stop');
  var live = document.getElementById('rec-live');
  var timeOut = document.getElementById('rec-time');
  var sentOut = document.getElementById('rec-sent');
  var msg = document.getElementById('rec-msg');
  var fileInput = document.getElementById('rec-file');

  // Выбор файла отправляет форму сам: лишний шаг «теперь нажмите загрузить»
  // на телефоне только раздражает.
  if (fileInput) {
    fileInput.addEventListener('change', function () {
      if (fileInput.files && fileInput.files.length) form.submit();
    });
  }

  var recorder = null, stream = null, wakeLock = null;
  var recordingId = 0, nextSeq = 0, sending = false, queue = [], finished = false;
  var startedAt = 0, ticker = 0;
  // Пропуски: система выключила микрофон — запись идёт, а звука нет. Время
  // пропуска не считается записанным, и место каждого уходит на сервер,
  // чтобы конспект не сшил края пропуска в одну мысль.
  var gaps = [], gapStart = 0, gapAt = 0, lostMs = 0;

  function say(text) { if (msg) msg.textContent = text || ''; }

  function post(url, body, type) {
    return fetch(url, {
      method: body instanceof Blob ? 'PUT' : 'POST',
      headers: type ? { 'Content-Type': type } : undefined,
      body: body,
      credentials: 'same-origin'
    }).then(function (r) {
      if (!r.ok) return r.json().catch(function () { return {}; }).then(function (j) {
        throw new Error(j.error || ('сервер ответил ' + r.status));
      });
      return r.json();
    });
  }

  function lessonValue() {
    var sel = form.querySelector('select[name="lesson"]');
    return sel ? sel.value : '';
  }

  startBtn.addEventListener('click', function () {
    startBtn.disabled = true;
    say('Разрешите доступ к микрофону…');
    navigator.mediaDevices.getUserMedia({
      audio: {
        channelCount: 1,
        // Шумодав и автоусиление рассчитаны на разговор у рта; в аудитории
        // они срезают преподавателя как фоновый шум.
        noiseSuppression: false,
        autoGainControl: true,
        echoCancellation: false
      }
    }).then(function (s) {
      stream = s;
      return post('/api/v1/notes/recordings', JSON.stringify({
        group: form.querySelector('input[name="group"]').value,
        discipline: form.querySelector('input[name="d"]').value,
        lesson: lessonValue()
      }), 'application/json');
    }).then(function (rec) {
      recordingId = rec.id;
      begin();
    }).catch(function (err) {
      startBtn.disabled = false;
      stopTracks();
      say(err && err.name === 'NotAllowedError'
        ? 'Микрофон запрещён в настройках браузера — записать не получится, но файл можно загрузить.'
        : 'Не вышло начать запись: ' + (err.message || err));
    });
  });

  function begin() {
    var mime = pickMime();
    // Битрейт низкий сознательно: речь в моно, полтора часа — это около
    // шестнадцати мегабайт, которые уедут по мобильной сети незаметно.
    var opts = { audioBitsPerSecond: 24000 };
    if (mime) opts.mimeType = mime;
    try { recorder = new MediaRecorder(stream, opts); }
    catch (e) { recorder = new MediaRecorder(stream); }

    recorder.ondataavailable = function (e) {
      if (e.data && e.data.size) { queue.push(e.data); pump(); }
    };
    recorder.onstop = function () {
      if (!finished) {
        // Остановил не человек, а система: микрофон отобран насовсем.
        finished = true;
        say('Система отключила микрофон — сохраняем то, что успели записать.');
      }
      finalize();
    };
    stream.getAudioTracks().forEach(function (t) {
      t.addEventListener('mute', startGap);
      t.addEventListener('unmute', endGap);
    });
    recorder.start(15000);

    startedAt = Date.now();
    live.hidden = false;
    startBtn.hidden = true;
    say(isIOS
      ? 'Идёт запись. Держите приложение открытым: свёрнутому iOS выключает микрофон.'
      : 'Идёт запись. Экран можно гасить, приложение — сворачивать.');
    ticker = setInterval(tick, 1000);
    tick();
    keepAwake();
    window.addEventListener('beforeunload', warn);
    document.addEventListener('visibilitychange', keepAwake);
    document.addEventListener('visibilitychange', watchHidden);
  }

  // На айфоне свёрнутое приложение теряет микрофон, даже если событие mute
  // не пришло: страница просто засыпает. Поэтому там уход с экрана сам по
  // себе считается началом пропуска. На Android запись в фоне идёт, и
  // пропуск отмечается только по mute.
  function watchHidden() {
    if (!recorder || finished) return;
    if (document.visibilityState === 'hidden') {
      if (isIOS) startGap();
    } else if (!trackMuted()) {
      endGap();
    }
  }

  function trackMuted() {
    if (!stream) return false;
    var tracks = stream.getAudioTracks();
    return tracks.length > 0 && tracks[0].muted;
  }

  function recordedMs(now) {
    return now - startedAt - lostMs - (gapStart ? now - gapStart : 0);
  }

  function startGap() {
    if (gapStart || !recorder || finished) return;
    var now = Date.now();
    gapAt = Math.max(0, Math.floor(recordedMs(now) / 1000));
    gapStart = now;
    tick();
  }

  function endGap() {
    if (!gapStart) return;
    var now = Date.now(), from = gapStart, dur = now - gapStart;
    gapStart = 0;
    lostMs += dur;
    tick();
    // Меньше пяти секунд — переключились туда-обратно, говорить не о чем.
    if (dur < 5000) return;
    gaps.push({ at_sec: gapAt, dur_sec: Math.round(dur / 1000) });
    say('Запись прерывалась с ' + hhmm(from) + ' до ' + hhmm(now) + ' (около ' +
      Math.max(1, Math.round(dur / 60000)) + ' мин): система выключала микрофон. ' +
      'В конспекте это место будет отмечено.');
  }

  function hhmm(ms) {
    var d = new Date(ms);
    return d.getHours() + ':' + (d.getMinutes() < 10 ? '0' : '') + d.getMinutes();
  }

  function pickMime() {
    var want = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4', 'audio/ogg;codecs=opus'];
    for (var i = 0; i < want.length; i++) {
      if (window.MediaRecorder.isTypeSupported && window.MediaRecorder.isTypeSupported(want[i])) return want[i];
    }
    return '';
  }

  // tick показывает записанное время, а не прошедшее: во время пропуска
  // часы стоят и помечены, чтобы не обещать звук, которого нет.
  function tick() {
    var sec = Math.max(0, Math.floor(recordedMs(Date.now()) / 1000));
    var m = Math.floor(sec / 60), s = sec % 60;
    if (timeOut) timeOut.textContent = m + ':' + (s < 10 ? '0' : '') + s + (gapStart ? ' · пауза' : '');
  }

  // pump отправляет куски строго по одному и по порядку: сервер склеивает
  // их в исходный поток, и кусок, обогнавший предыдущий, ломает файл.
  function pump() {
    if (sending || !queue.length || !recordingId) return;
    sending = true;
    var blob = queue[0];
    post('/api/v1/notes/recordings/' + recordingId + '/chunks/' + nextSeq, blob, '')
      .then(function (rec) {
        queue.shift();
        nextSeq++;
        sending = false;
        if (sentOut) sentOut.textContent = 'отправлено ' + Math.round(rec.bytes / 1024) + ' КБ';
        pump();
        if (finished && !queue.length) finish();
      })
      .catch(function (err) {
        sending = false;
        // Сеть в аудитории пропадает постоянно; кусок остаётся в очереди и
        // уедет со следующей попыткой, а запись не прерывается.
        say('Связь потерялась, повторяем… (' + (err.message || err) + ')');
        setTimeout(pump, 4000);
      });
  }

  stopBtn.addEventListener('click', function () {
    stopBtn.disabled = true;
    finished = true;
    say('Досылаем запись…');
    if (recorder && recorder.state !== 'inactive') recorder.stop();
  });

  function finalize() {
    endGap();
    document.removeEventListener('visibilitychange', watchHidden);
    clearInterval(ticker);
    stopTracks();
    releaseWake();
    window.removeEventListener('beforeunload', warn);
    if (!queue.length && !sending) finish(); else pump();
  }

  function finish() {
    if (!recordingId) return;
    var id = recordingId;
    recordingId = 0;
    post('/api/v1/notes/recordings/' + id + '/finish', JSON.stringify({ gaps: gaps }), 'application/json')
      .then(function () {
        var base = window.location.pathname + window.location.search.replace(/&rec=\d+/, '');
        window.location.href = base + (base.indexOf('?') >= 0 ? '&' : '?') + 'rec=' + id;
      })
      .catch(function (err) { say('Запись сохранена, но закрыть её не вышло: ' + (err.message || err)); });
  }

  function stopTracks() {
    if (stream) { stream.getTracks().forEach(function (t) { t.stop(); }); stream = null; }
  }

  function warn(e) { e.preventDefault(); e.returnValue = ''; return ''; }

  // Экран гаснет — вкладка засыпает, и запись обрывается. Wake Lock держит
  // его включённым; после сворачивания блокировку снимает сам браузер,
  // поэтому её берут заново при возвращении.
  function keepAwake() {
    if (!navigator.wakeLock || document.visibilityState !== 'visible' || !recorder) return;
    navigator.wakeLock.request('screen').then(function (lock) { wakeLock = lock; }).catch(function () {});
  }
  function releaseWake() {
    document.removeEventListener('visibilitychange', keepAwake);
    if (wakeLock) { wakeLock.release().catch(function () {}); wakeLock = null; }
  }

  pollRecording();

  // pollRecording обновляет экран записи, пока она обрабатывается: человек
  // и так ждёт, заставлять его жать F5 невежливо.
  function pollRecording() {
    var box = document.getElementById('rec');
    if (!box) return;
    var id = box.getAttribute('data-rec');
    var status = box.getAttribute('data-status');
    if (!id || status === 'ready' || status === 'failed' || status === 'uploading') return;
    setTimeout(function () {
      fetch('/api/v1/notes/recordings/' + id, { credentials: 'same-origin' })
        .then(function (r) { return r.json(); })
        .then(function (rec) {
          if (rec.status !== status) { window.location.reload(); return; }
          pollRecording();
        })
        .catch(function () { pollRecording(); });
    }, 15000);
  }
})();
