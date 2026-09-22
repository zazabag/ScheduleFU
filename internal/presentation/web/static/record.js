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
    recorder.onstop = function () { finalize(); };
    recorder.start(15000);

    startedAt = Date.now();
    live.hidden = false;
    startBtn.hidden = true;
    say('Идёт запись. Экран можно погасить только на Android — на айфоне держите приложение открытым.');
    ticker = setInterval(tick, 1000);
    tick();
    keepAwake();
    window.addEventListener('beforeunload', warn);
    document.addEventListener('visibilitychange', keepAwake);
  }

  function pickMime() {
    var want = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4', 'audio/ogg;codecs=opus'];
    for (var i = 0; i < want.length; i++) {
      if (window.MediaRecorder.isTypeSupported && window.MediaRecorder.isTypeSupported(want[i])) return want[i];
    }
    return '';
  }

  function tick() {
    var sec = Math.floor((Date.now() - startedAt) / 1000);
    var m = Math.floor(sec / 60), s = sec % 60;
    if (timeOut) timeOut.textContent = m + ':' + (s < 10 ? '0' : '') + s;
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
    post('/api/v1/notes/recordings/' + id + '/finish', '')
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
