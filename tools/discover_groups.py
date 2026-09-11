"""Сбор справочника групп ruz.fa.ru перебором «год-номер» и буквенных префиксов.

Поиск работает по подстроке от начала токена, поэтому «24-1» находит все
группы с таким хвостом независимо от буквенного префикса. Это даёт полный
охват без угадывания сокращений направлений.

Результат: data/groups.json
"""
import json, time, threading, urllib.request, urllib.parse
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

UA = {'User-Agent': 'Mozilla/5.0 (ScheduleFU/0.1 discovery)'}
OUT = Path(__file__).resolve().parent.parent / 'data' / 'groups.json'
_lock = threading.Lock()
found = {}


def api(term):
    url = 'https://ruz.fa.ru/api/search?' + urllib.parse.urlencode(
        {'term': term, 'type': 'group'})
    for attempt in range(3):
        try:
            with urllib.request.urlopen(
                    urllib.request.Request(url, headers=UA), timeout=30) as r:
                return json.load(r)
        except Exception:
            if attempt == 2:
                return []
            time.sleep(1.5 * (attempt + 1))


def work(term):
    for x in api(term) or []:
        with _lock:
            found[int(x['id'])] = {
                'id': int(x['id']),
                'name': x.get('label', '').strip(),
                # description у группы — числовой oid факультета; названия
                # факультета API не отдаёт нигде.
                'faculty_oid': (x.get('description') or '').strip(),
            }
    time.sleep(0.15)


terms = [f'{y}-{n}' for y in range(19, 27) for n in range(1, 26)]
with ThreadPoolExecutor(max_workers=4) as ex:
    list(ex.map(work, terms))

OUT.parent.mkdir(exist_ok=True)
OUT.write_text(json.dumps({
    'generated_at': time.strftime('%Y-%m-%dT%H:%M:%S'),
    'terms_probed': len(terms),
    'groups': [found[k] for k in sorted(found)],
}, ensure_ascii=False, indent=2), encoding='utf-8')
print(f'групп: {len(found)} (перебрано {len(terms)} префиксов) -> {OUT}')
