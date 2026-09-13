// ScheduleFU — версия для GitHub Pages.
//
// Pages раздаёт только файлы, поэтому вся работа идёт в браузере: данные
// приходят одним файлом на день, а фильтрация, расчёт «свободно сейчас» и
// поиск считаются здесь. Логика занятости повторяет internal/web/slots.go —
// это осознанное дублирование: на Pages запустить Go нечем.

'use strict';

var state = { meta: null, day: null, dayKey: null };

// ——— загрузка ———

function loadJSON(path) {
  return fetch(path, { cache: 'no-cache' }).then(function (r) {
    if (!r.ok) throw new Error(path + ': ' + r.status);
    return r.json();
  });
}

function todayKey() {
  var d = new Date();
  return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
}

function pad(n) { return n < 10 ? '0' + n : String(n); }

function nowClock() {
  var d = new Date();
  return pad(d.getHours()) + ':' + pad(d.getMinutes());
}

// pickDay выбирает день из выгруженных: сегодняшний, а если данных за
// сегодня нет — ближайший следующий, иначе последний доступный.
function pickDay(days, wanted) {
  if (days.indexOf(wanted) !== -1) return wanted;
  for (var i = 0; i < days.length; i++) {
    if (days[i] >= wanted) return days[i];
  }
  return days[days.length - 1];
}

function ensureDay(key) {
  if (state.dayKey === key && state.day) return Promise.resolve(state.day);
  return loadJSON('data/day/' + key + '.json').then(function (d) {
    state.day = d;
    state.dayKey = key;
    return d;
  });
}

// ——— занятость ———

// Клетка занята, если пара ПЕРЕСЕКАЕТСЯ со слотом, а не начинается вместе
// с ним: около двух процентов пар идут вне сетки, вплоть до 08:00—22:00.
function overlaps(aFrom, aTo, bFrom, bTo) {
  return aFrom < bTo && bFrom < aTo;
}

function buildCells(lessons, now) {
  return state.meta.slots.map(function (s) {
    var busy = null;
    for (var i = 0; i < lessons.length; i++) {
      if (overlaps(s.Begins, s.Ends, lessons[i].b, lessons[i].e)) { busy = lessons[i]; break; }
    }
    var cls = busy ? 'busy' : (s.Ends <= now ? 'past' : 'free');
    var title = busy ? busy.b + '—' + busy.e + ' · ' + busy.d + (busy.l ? ' · ' + busy.l : '') : '';
    // title вставляется через esc() на месте использования.
    return { cls: cls, label: String(s.Begins).replace(/^0/, ''), title: title };
  });
}

function freeNow(lessons, now) {
  for (var i = 0; i < lessons.length; i++) {
    if (lessons[i].b <= now && now < lessons[i].e) return false;
  }
  return true;
}

function freeUntil(lessons, now) {
  for (var i = 0; i < lessons.length; i++) {
    if (lessons[i].b > now) return lessons[i].b;
  }
  return '';
}

// ——— вспомогательное ———

function el(html) {
  var t = document.createElement('template');
  t.innerHTML = html.trim();
  return t.content.firstChild;
}

function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
    return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
  });
}

function lessonsByRoom(day) {
  var map = {};
  day.lessons.forEach(function (l) {
    if (!l.a) return;
    (map[l.a] = map[l.a] || []).push(l);
  });
  return map;
}

function store(key, value) {
  try {
    if (value === undefined) return localStorage.getItem(key);
    localStorage.setItem(key, value);
  } catch (e) { /* приватный режим — просто не запоминаем */ }
  return value;
}

function freshness() {
  if (!state.day) return '';
  var d = new Date(state.day.generated_at);
  if (isNaN(d)) return '';
  var mins = Math.round((Date.now() - d.getTime()) / 60000);
  if (mins < 60) return mins + ' мин назад';
  if (mins < 60 * 24) return Math.round(mins / 60) + ' ч назад';
  return Math.round(mins / 60 / 24) + ' дн назад';
}

var MONTHS = ['января','февраля','марта','апреля','мая','июня','июля',
              'августа','сентября','октября','ноября','декабря'];
var DOW = ['вс','пн','вт','ср','чт','пт','сб'];
var DOW_FULL = ['воскресенье','понедельник','вторник','среда','четверг','пятница','суббота'];

function dateRu(key) {
  var p = key.split('-');
  return parseInt(p[2], 10) + ' ' + MONTHS[parseInt(p[1], 10) - 1];
}

function dowOf(key) {
  var p = key.split('-');
  return new Date(p[0], parseInt(p[1], 10) - 1, p[2]).getDay();
}

// ——— экран: свободные аудитории ———

function renderRooms(app) {
  var now = nowClock();
  var site = store('site') || (state.meta.sites[0] && state.meta.sites[0].v) || '';
  var floor = store('floor') || '';
  var byRoom = lessonsByRoom(state.day);

  var rooms = state.meta.auditoriums.filter(function (a) { return a.s === site; });
  var floors = [];
  rooms.forEach(function (a) {
    if (a.f != null && floors.indexOf(a.f) === -1) floors.push(a.f);
  });
  floors.sort(function (x, y) { return x - y; });

  var free = [];
  rooms.forEach(function (a) {
    var ls = byRoom[a.o] || [];
    if (!freeNow(ls, now)) return;
    if (floor !== '' && String(a.f) !== floor) return;
    free.push({ a: a, ls: ls });
  });

  var total = rooms.length;
  var freeCount = rooms.filter(function (a) { return freeNow(byRoom[a.o] || [], now); }).length;
  var label = (state.meta.sites.filter(function (c) { return c.v === site; })[0] || {}).l || '';

  var html = '' +
    '<div class="head">' +
      '<div class="head-row">' +
        '<div><h1 class="title">Свободно сейчас</h1>' +
        '<p class="subtitle">' + esc(label) + ' · ' + freeCount + ' из ' + total + '</p></div>' +
        '<div class="clock">' + now + '</div>' +
      '</div>' +
      '<div class="tabs">' +
        state.meta.sites.map(function (c) {
          return '<a class="tab' + (c.v === site ? ' on' : '') + '" href="#/rooms" data-site="' +
            esc(c.v) + '">' + esc(c.l) + '</a>';
        }).join('') +
      '</div>' +
      (floors.length ? '<div class="chips"><span class="chips-label">этаж</span>' +
        '<a class="chip' + (floor === '' ? ' on' : '') + '" href="#/rooms" data-floor="">все</a>' +
        floors.map(function (f) {
          return '<a class="chip' + (String(f) === floor ? ' on' : '') +
            '" href="#/rooms" data-floor="' + f + '">' + f + '</a>';
        }).join('') + '</div>' : '') +
    '</div>' +
    '<div class="list">' +
      (free.length ? free.map(function (r) {
        var cells = buildCells(r.ls, now);
        var until = freeUntil(r.ls, now);
        var meta = [r.a.b];
        if (r.a.f != null) meta.push(String(r.a.f) + ' эт');
        if (r.a.cap) meta.push(String(r.a.cap) + ' мест');
        return '<details class="room"><summary>' +
          '<div class="room-head">' +
            '<div class="room-id"><span class="room-num">' + esc(r.a.r) + '</span>' +
            '<span class="room-meta">' + esc(meta.join(' · ')) + '</span></div>' +
            '<span class="room-until' + (until ? ' warn' : '') + '">' +
              (until ? 'до ' + esc(until) + '' : 'до конца дня') + '</span>' +
          '</div>' +
          '<div class="rail">' + cells.map(function (c) {
            return '<div class="rail-cell ' + c.cls + '"' + (c.title ? ' title="' + esc(c.title) + '"' : '') +
              '><div class="rail-box"></div><div class="rail-time">' + esc(c.label) + '</div></div>';
          }).join('') + '</div>' +
        '</summary><div class="room-lessons">' +
          (r.ls.length ? r.ls.map(function (l) {
            return '<div class="room-lesson"><span class="room-lesson-time">' + esc(l.b) + '—' + esc(l.e) +
              '</span><span class="room-lesson-what">' + esc(l.d) + (l.l ? ' · ' + esc(l.l) : '') + '</span></div>';
          }).join('') : '<div class="room-empty">В этот день пар нет</div>') +
        '</div></details>';
      }).join('') : '<div class="empty"><div class="empty-title">Свободных нет</div>' +
        '<div class="empty-sub">Попробуйте другой корпус или этаж</div></div>') +
    '</div>' +
    '<div class="legend">' +
      '<div class="legend-item"><div class="legend-box free"></div><span class="legend-text">свободна</span></div>' +
      '<div class="legend-item"><div class="legend-box busy"></div><span class="legend-text">пара</span></div>' +
      '<div class="legend-item"><div class="legend-box past"></div><span class="legend-text">прошло</span></div>' +
    '</div>' +
    '<div class="note">' + dayNote() +
      ' Брони вне расписания (мероприятия, экзамены) вуз не публикует — аудитория может быть занята.</div>';

  app.innerHTML = html;

  app.querySelectorAll('[data-site]').forEach(function (a) {
    a.addEventListener('click', function () {
      store('site', a.getAttribute('data-site'));
      store('floor', '');
      render();
    });
  });
  app.querySelectorAll('[data-floor]').forEach(function (a) {
    a.addEventListener('click', function () {
      store('floor', a.getAttribute('data-floor'));
      render();
    });
  });
}

// dayNote честно предупреждает, если показывается не сегодняшний день:
// на Pages данные обновляются выкладкой, а не сами собой.
function dayNote() {
  if (state.dayKey === todayKey()) {
    return 'Данные собраны ' + freshness() + '.';
  }
  return 'Данных за сегодня нет — показан ' + dateRu(state.dayKey) + '. ';
}

// ——— экран: расписание группы ———

function renderSchedule(app) {
  var group = store('group') || '';
  if (!group) { renderGroups(app); return; }

  var day = state.dayKey;
  var lessons = state.day.lessons.filter(function (l) {
    return l.g && l.g.indexOf(group) !== -1;
  });

  var html = '' +
    '<div class="head">' +
      '<div class="head-row">' +
        '<div><h1 class="title">' + esc(group) + '</h1>' +
        '<p class="subtitle">' + dateRu(day) + ' · ' + DOW_FULL[dowOf(day)] + '</p></div>' +
        '<a class="tab" href="#/groups">Сменить</a>' +
      '</div>' +
      '<div class="days">' + state.meta.days.map(function (d) {
        return '<a class="day' + (d === day ? ' on' : '') + '" href="#/schedule" data-day="' + d + '">' +
          '<span class="day-dow">' + DOW[dowOf(d)] + '</span>' +
          '<span class="day-num">' + parseInt(d.split('-')[2], 10) + '</span>' +
          '<span class="day-dot"></span></a>';
      }).join('') + '</div>' +
    '</div>' +
    '<div class="list">' +
      (lessons.length ? lessons.map(function (l) {
        var a = roomOf(l.a);
        return '<div class="lesson">' +
          '<div class="lesson-time"><span class="lesson-from">' + esc(l.b) + '</span>' +
          '<span class="lesson-to">' + esc(l.e) + '</span></div>' +
          '<div class="lesson-rail"></div>' +
          '<div class="lesson-body">' +
            '<div class="lesson-discipline">' + esc(l.d) + '</div>' +
            '<div class="lesson-kind">' + esc(shortKind(l.k)) + (l.l ? ' · ' + esc(l.l) : '') + '</div>' +
            '<div class="lesson-where"><span class="lesson-room">' + esc(a ? a.r : '—') + '</span>' +
            '<span class="lesson-place">' + esc(a ? a.b + (a.f != null ? ' · ' + a.f + ' эт' : '') : '') + '</span></div>' +
          '</div></div>';
      }).join('') : '<div class="empty"><div class="empty-title">Пар нет</div>' +
        '<div class="empty-sub">Свободный день</div></div>') +
    '</div>' +
    '<div class="note">' + dayNote() + '</div>';

  app.innerHTML = html;
  app.querySelectorAll('[data-day]').forEach(function (a) {
    a.addEventListener('click', function (e) {
      e.preventDefault();
      ensureDay(a.getAttribute('data-day')).then(render);
    });
  });
}

function roomOf(oid) {
  if (!oid) return null;
  if (!state.roomIndex) {
    state.roomIndex = {};
    state.meta.auditoriums.forEach(function (a) { state.roomIndex[a.o] = a; });
  }
  return state.roomIndex[oid];
}

function shortKind(k) {
  if (!k) return '';
  if (/Практические|семинар/i.test(k)) return 'Семинар';
  if (/Лекц/i.test(k)) return 'Лекция';
  if (/Лаборатор/i.test(k)) return 'Лабораторная';
  if (/Экзамен/i.test(k)) return 'Экзамен';
  if (/Зач[её]т/i.test(k)) return 'Зачёт';
  return k;
}

// ——— экран: выбор группы ———

function renderGroups(app) {
  var q = (store('groupQuery') || '').trim();
  var course = store('course') || '';
  var chosen = store('group') || '';

  var found = state.meta.groups.filter(function (g) {
    if (q && g.n.toLowerCase().indexOf(q.toLowerCase()) === -1) return false;
    if (course && String(g.c) !== course) return false;
    return true;
  }).slice(0, 200);

  app.innerHTML = '' +
    '<div class="head">' +
      '<div class="head-row"><div>' +
        '<h1 class="title">Ваша группа</h1>' +
        '<p class="subtitle">Запомним в браузере. Без логина и пароля.</p>' +
      '</div></div>' +
      '<div class="search"><input type="search" id="q" value="' + esc(q) +
        '" placeholder="Например, ПИ24-1" autocomplete="off"></div>' +
      '<div class="chips"><span class="chips-label">курс</span>' +
        ['', '1', '2', '3', '4', '5'].map(function (c) {
          return '<a class="chip' + (c === course ? ' on' : '') + '" href="#/groups" data-course="' + c + '">' +
            (c === '' ? 'все' : c) + '</a>';
        }).join('') +
      '</div>' +
    '</div>' +
    '<div class="list">' +
      '<div class="note" style="padding-left:0">найдено: ' + found.length + '</div>' +
      (found.length ? found.map(function (g) {
        return '<a class="pick' + (g.n === chosen ? ' on' : '') + '" href="#/schedule" data-group="' + esc(g.n) + '">' +
          '<span><span class="pick-name">' + esc(g.n) + '</span><br>' +
          '<span class="pick-meta">' + (g.c ? g.c + ' курс' : 'курс не определён') + '</span></span>' +
          (g.n === chosen ? '<span style="color:var(--accent)">✓</span>' : '') + '</a>';
      }).join('') : '<div class="empty"><div class="empty-title">Ничего не нашлось</div>' +
        '<div class="empty-sub">Проверьте написание — например, ПИ24-1</div></div>') +
    '</div>';

  var input = document.getElementById('q');
  input.addEventListener('input', function () {
    store('groupQuery', input.value);
    var pos = input.selectionStart;
    renderGroups(app);
    var again = document.getElementById('q');
    again.focus();
    again.setSelectionRange(pos, pos);
  });
  app.querySelectorAll('[data-course]').forEach(function (a) {
    a.addEventListener('click', function () { store('course', a.getAttribute('data-course')); renderGroups(app); });
  });
  app.querySelectorAll('[data-group]').forEach(function (a) {
    a.addEventListener('click', function () { store('group', a.getAttribute('data-group')); });
  });
}

// ——— экран: преподаватели ———

function renderLecturers(app) {
  var q = (store('lectQuery') || '').trim();
  var oid = store('lectOid') || '';
  var now = nowClock();

  var found = q ? state.meta.lecturers.filter(function (l) {
    return l.n.toLowerCase().indexOf(q.toLowerCase()) !== -1;
  }).slice(0, 40) : [];

  var body = '';
  if (oid) {
    var lessons = state.day.lessons.filter(function (l) { return String(l.lo) === oid; });
    var name = (state.meta.lecturers.filter(function (l) { return String(l.o) === oid; })[0] || {}).n || '';
    var current = lessons.filter(function (l) { return l.b <= now && now < l.e; })[0];
    var next = lessons.filter(function (l) { return l.b > now; })[0];
    var a = current ? roomOf(current.a) : null;

    body = '<div class="now-card' + (current ? '' : ' idle') + '">' +
      '<div class="head-row"><div>' +
        '<div class="pick-name">' + esc(name) + '</div>' +
        '<div class="pick-meta">' + dateRu(state.dayKey) + ' · ' + DOW_FULL[dowOf(state.dayKey)] + '</div>' +
      '</div><span class="badge' + (current ? '' : ' idle') + '">' +
        (current ? 'на паре' : 'вне пар') + '</span></div>' +
      (current ?
        '<div class="now-where"><div><div class="now-room">' + esc(a ? a.r : '—') + '</div>' +
        '<div class="pick-meta">' + esc(a ? a.b + (a.f != null ? ' · ' + a.f + ' эт' : '') : '') + '</div></div>' +
        '<div><div class="lesson-kind" style="color:var(--text)">' + esc(current.b) + '—' + esc(current.e) + '</div>' +
        '<div class="room-lesson-what">' + esc(current.d) + '</div>' +
        '<div class="pick-meta">' + esc((current.g || []).join(', ')) + '</div></div></div>'
        : '<div class="now-where"><span class="lesson-kind">' +
          (next ? 'Сейчас пары нет. Ближайшая в ' + esc(next.b) + '.'
                : (lessons.length ? 'Пары на этот день закончились.' : 'В этот день пар нет.')) +
          '</span></div>') +
    '</div>' +
    lessons.map(function (l) {
      var r = roomOf(l.a);
      return '<div class="lesson"><div class="lesson-time">' +
        '<span class="lesson-from">' + esc(l.b) + '</span><span class="lesson-to">' + esc(l.e) + '</span></div>' +
        '<div class="lesson-rail"></div><div class="lesson-body">' +
        '<div class="lesson-discipline">' + esc(l.d) + '</div>' +
        '<div class="lesson-where"><span class="lesson-room">' + esc(r ? r.r : '—') + '</span>' +
        '<span class="lesson-place">' + esc(r ? r.b : '') + '</span></div></div></div>';
    }).join('');
  } else {
    body = found.length ? found.map(function (l) {
      return '<a class="pick" href="#/lecturers" data-oid="' + l.o + '"><span class="pick-name">' +
        esc(l.n) + '</span></a>';
    }).join('') : '<div class="empty"><div class="empty-title">' +
      (q ? 'Никого не нашлось' : 'Найдите преподавателя') + '</div>' +
      '<div class="empty-sub">' + (q ? 'Проверьте написание фамилии' : 'Введите фамилию в поиск') + '</div></div>';
  }

  app.innerHTML = '' +
    '<div class="head">' +
      '<div class="head-row"><div><h1 class="title">Преподаватели</h1>' +
      '<p class="subtitle">Где преподаватель сейчас</p></div>' +
      '<div class="clock">' + now + '</div></div>' +
      '<div class="search"><input type="search" id="lq" value="' + esc(q) +
        '" placeholder="Фамилия" autocomplete="off"></div>' +
    '</div>' +
    '<div class="list">' + body + '</div>' +
    '<div class="note">Данные из открытого расписания вуза. Показывает пары, а не человека: ' +
    'вне пар местонахождение неизвестно.</div>';

  var input = document.getElementById('lq');
  input.addEventListener('input', function () {
    store('lectQuery', input.value);
    store('lectOid', '');
    var pos = input.selectionStart;
    renderLecturers(app);
    var again = document.getElementById('lq');
    again.focus();
    again.setSelectionRange(pos, pos);
  });
  app.querySelectorAll('[data-oid]').forEach(function (a) {
    a.addEventListener('click', function () { store('lectOid', a.getAttribute('data-oid')); renderLecturers(app); });
  });
}

// ——— маршрутизация ———

function route() {
  var h = (location.hash || '#/rooms').replace('#/', '');
  return ['rooms', 'schedule', 'groups', 'lecturers'].indexOf(h) !== -1 ? h : 'rooms';
}

function render() {
  var app = document.getElementById('app');
  var r = route();
  ['rooms', 'schedule', 'lecturers'].forEach(function (t) {
    var link = document.getElementById('nav-' + t);
    if (link) link.className = (t === r || (r === 'groups' && t === 'schedule')) ? 'on' : '';
  });
  if (r === 'rooms') renderRooms(app);
  else if (r === 'schedule') renderSchedule(app);
  else if (r === 'groups') renderGroups(app);
  else renderLecturers(app);
}

window.addEventListener('hashchange', render);

loadJSON('data/meta.json')
  .then(function (meta) {
    state.meta = meta;
    return ensureDay(pickDay(meta.days, todayKey()));
  })
  .then(render)
  .catch(function (err) {
    document.getElementById('app').innerHTML =
      '<div class="empty"><div class="empty-title">Не удалось загрузить данные</div>' +
      '<div class="empty-sub">' + esc(err.message) + '</div></div>';
  });


// ——— установка на устройство и уведомления ———

// Service worker регистрируется после загрузки страницы: во время первого
// показа он не нужен, а конкуренция за сеть замедлила бы открытие.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', function () {
    navigator.serviceWorker.register('sw.js').catch(function (e) {
      // Регистрация падает при открытии по file:// и в приватном режиме.
      // Это не мешает приложению работать, поэтому только пишем в консоль.
      console.warn('офлайн-режим недоступен:', e && e.message);
    });
  });
}

// Предложение установить приложение показываем не сразу: браузер сам
// решает, когда посетитель «свой», и до этого кнопка была бы шумом.
var installPrompt = null;
window.addEventListener('beforeinstallprompt', function (e) {
  e.preventDefault();
  installPrompt = e;
  var bar = document.getElementById('install-bar');
  if (bar) bar.hidden = false;
});

function askInstall() {
  if (!installPrompt) return;
  installPrompt.prompt();
  installPrompt.userChoice.then(function () {
    installPrompt = null;
    var bar = document.getElementById('install-bar');
    if (bar) bar.hidden = true;
  });
}

// Разрешение на уведомления спрашивается только по явному действию:
// запрос при загрузке страницы браузеры справедливо считают спамом и
// показывают его всё менее заметно.
function askNotifications() {
  if (!('Notification' in window)) return Promise.resolve('unsupported');
  if (Notification.permission === 'granted') return Promise.resolve('granted');
  return Notification.requestPermission();
}

window.ScheduleFU = { askInstall: askInstall, askNotifications: askNotifications };
