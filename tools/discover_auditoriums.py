"""
Разовый сбор справочника аудиторий ruz.fa.ru.

Две фазы:
  A. /api/search?type=auditorium по префиксам — даёт тип аудитории
     (Лекционная / Лаборатория / Коворкинг ...), которого нет в расписании.
  B. перебор auditoriumOid через /api/schedule/auditorium — даёт полноту
     и вместимость; ловит аудитории, которые поиск не показал.

Результат: data/auditoriums.json

Запускается редко (фонд меняется раз в семестр), поэтому оптимизировать
нечего — важнее не долбить чужой сервер: небольшой пул потоков и пауза.
"""
import argparse, json, itertools, string, sys, threading, time
import urllib.request, urllib.parse, urllib.error
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

BASE = 'https://ruz.fa.ru'
UA = {'User-Agent': 'Mozilla/5.0 (ScheduleFU/0.1 discovery)'}
OUT = Path(__file__).resolve().parent.parent / 'data' / 'auditoriums.json'

_lock = threading.Lock()
_stats = {'req': 0, 'err': 0}


def api(path, **params):
    url = BASE + path + ('?' + urllib.parse.urlencode(params) if params else '')
    for attempt in range(3):
        try:
            req = urllib.request.Request(url, headers=UA)
            with urllib.request.urlopen(req, timeout=30) as r:
                data = json.load(r)
            with _lock:
                _stats['req'] += 1
            return data
        except Exception:
            if attempt == 2:
                with _lock:
                    _stats['err'] += 1
                return None
            time.sleep(1.5 * (attempt + 1))


def parse_name(name):
    """ЛП49/2/313 -> (корпус 'ЛП49/2', номер '313', этаж 3).

    Этаж берём из номера, потому что auditoriumfloor из API всегда 0.
    Считаем этажом первую цифру трёх- или четырёхзначного номера.
    """
    parts = name.split('/')
    room = parts[-1].strip()
    prefix = '/'.join(parts[:-1]).strip() if len(parts) > 1 else ''
    floor = None
    digits = ''.join(itertools.takewhile(str.isdigit, room.lstrip('ауд.').strip()))
    if len(digits) in (3, 4):
        floor = int(digits[0])
    return prefix, room, floor


def phase_a(pause):
    """Поиск по префиксам: собираем тип аудитории и корпус."""
    terms = set()
    # Буквенные префиксы корпусов и типовые слова.
    terms.update(['ауд', 'зал', 'каб', 'лаб', 'Коворкинг', 'Ленинградский',
                  'Вешняковский', 'Масловка', 'Щербаковская', 'Кибальчича',
                  'Касаткина', 'Олеко', 'Дундича', 'Виртуальное'])
    # Трёхсимвольные номерные префиксы: 100..999 покрывает нумерацию комнат.
    terms.update(str(n) for n in range(100, 1000))
    # Буквенные пары корпусных сокращений (ЛП49/2, ВШ, ...).
    terms.update(a + b + c for a, b, c in
                 itertools.product('ЛВЩКМО', 'ПШЕАИ', string.digits))

    found = {}
    terms = sorted(terms)

    def work(term):
        d = api('/api/search', term=term, type='auditorium')
        time.sleep(pause)
        if not d:
            return
        with _lock:
            for x in d:
                desc = [s.strip() for s in (x.get('description') or '').split('|')]
                found[int(x['id'])] = {
                    'oid': int(x['id']),
                    'name': x.get('label', '').strip(),
                    'building': desc[1] if len(desc) > 1 else None,
                    'kind': desc[2] if len(desc) > 2 else None,
                }

    with ThreadPoolExecutor(max_workers=4) as ex:
        for i, _ in enumerate(ex.map(work, terms), 1):
            if i % 100 == 0:
                print(f'  [A] {i}/{len(terms)} префиксов, найдено {len(found)}',
                      file=sys.stderr, flush=True)
    return found


def phase_b(oids, start, finish, pause):
    """Перебор oid: вместимость, корпус и подтверждение, что аудитория живая."""
    got = {}

    def work(oid):
        d = api(f'/api/schedule/auditorium/{oid}', start=start, finish=finish, lng=1)
        time.sleep(pause)
        if not d:
            return
        first = d[0]
        with _lock:
            got[oid] = {
                'oid': oid,
                'name': (first.get('auditorium') or '').strip(),
                'building': first.get('building'),
                'capacity': first.get('auditoriumAmount'),
                'lessons_in_probe': len(d),
            }

    oids = sorted(oids)
    with ThreadPoolExecutor(max_workers=6) as ex:
        for i, _ in enumerate(ex.map(work, oids), 1):
            if i % 200 == 0:
                print(f'  [B] {i}/{len(oids)} oid, живых {len(got)}',
                      file=sys.stderr, flush=True)
    return got


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--oid-max', type=int, default=4000)
    p.add_argument('--start', default='2026.09.07')
    p.add_argument('--finish', default='2026.09.13')
    p.add_argument('--pause', type=float, default=0.15,
                   help='пауза после каждого запроса в потоке, секунд')
    args = p.parse_args()

    t0 = time.time()
    print('Фаза A: поиск по префиксам...', file=sys.stderr, flush=True)
    a = phase_a(args.pause)
    print(f'Фаза A: {len(a)} аудиторий', file=sys.stderr, flush=True)

    print(f'Фаза B: перебор oid 1..{args.oid_max}...', file=sys.stderr, flush=True)
    b = phase_b(range(1, args.oid_max + 1), args.start, args.finish, args.pause)
    print(f'Фаза B: {len(b)} живых', file=sys.stderr, flush=True)

    merged = {}
    for oid in set(a) | set(b):
        rec = dict(a.get(oid, {}))
        rec.update({k: v for k, v in b.get(oid, {}).items() if v is not None})
        rec['oid'] = oid
        name = rec.get('name') or ''
        prefix, room, floor = parse_name(name)
        rec['name_prefix'], rec['room'], rec['floor'] = prefix, room, floor
        rec['seen_in'] = ('search' if oid in a else '') + ('+schedule' if oid in b else '')
        merged[oid] = rec

    OUT.parent.mkdir(exist_ok=True)
    OUT.write_text(json.dumps({
        'generated_at': time.strftime('%Y-%m-%dT%H:%M:%S'),
        'probe_window': [args.start, args.finish],
        'requests': _stats['req'], 'errors': _stats['err'],
        'auditoriums': [merged[k] for k in sorted(merged)],
    }, ensure_ascii=False, indent=2), encoding='utf-8')

    print(f'\nГотово за {time.time()-t0:.0f}с: {len(merged)} аудиторий -> {OUT}',
          f'({_stats["req"]} запросов, {_stats["err"]} ошибок)', file=sys.stderr)


if __name__ == '__main__':
    main()
