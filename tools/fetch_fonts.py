"""Шрифты оформлений — к себе в static/fonts.

Зачем: подключение стилей с fonts.googleapis.com блокирует отрисовку, а на
мобильной сети в России Google отвечает секундами. Замер с телефона
23.09.2026: страница пришла за 0,23 с, первая отрисовка — через 4,2 с, из
них 4,0 с — css2 с Google. Держим шрифты у себя: один сервер, кэш на год.

Берутся только кириллица и латиница, начертания — переменные (одним файлом
все насыщенности). Лицензия у всех семейств — SIL OFL, раздавать можно.

Запуск: python tools/fetch_fonts.py — перезаписывает static/fonts целиком.
"""
import hashlib
import re
import urllib.request
from pathlib import Path

OUT = Path(__file__).resolve().parent.parent / 'internal' / 'presentation' / 'web' / 'static' / 'fonts'
# Современный браузер: иначе Google отдаёт ttf вместо woff2.
UA = {'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 '
                    '(KHTML, like Gecko) Chrome/128.0 Safari/537.36'}
KEEP = {'cyrillic', 'cyrillic-ext', 'latin'}

# id файла -> параметр family для css2. Насыщенности — диапазоном: так
# Google отдаёт один переменный файл на подмножество.
FAMILIES = {
    'inter': 'Inter:wght@400..900',
    'jetbrains-mono': 'JetBrains+Mono:wght@400..700',
    'playfair-display': 'Playfair+Display:ital,wght@0,400..900;1,400..900',
    'rubik': 'Rubik:wght@400..900',
    'nunito': 'Nunito:wght@400..800',
}


def get(url):
    with urllib.request.urlopen(urllib.request.Request(url, headers=UA), timeout=60) as r:
        return r.read()


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    for old in OUT.iterdir():
        old.unlink()
    total = 0
    for fid, family in FAMILIES.items():
        css = get(f'https://fonts.googleapis.com/css2?family={family}&display=swap').decode()
        # Блоки идут парами «/* подмножество */ @font-face {…}».
        blocks = re.findall(r'/\* ([\w-]+) \*/\s*(@font-face \{.*?\})', css, re.S)
        out = [f'/* {family.split(":")[0].replace("+", " ")} — SIL OFL, с Google Fonts; tools/fetch_fonts.py */']
        n = 0
        for subset, block in blocks:
            if subset not in KEEP:
                continue
            url = re.search(r'url\((https://[^)]+\.woff2)\)', block).group(1)
            data = get(url)
            # Отпечаток содержимого в имени: сервер кэширует такие файлы на
            # год (web/server.go), как статику с ?v=.
            name = f'{fid}-{hashlib.sha256(data).hexdigest()[:10]}.woff2'
            (OUT / name).write_bytes(data)
            total += len(data)
            out.append(f'/* {subset} */\n' + block.replace(url, name))
            n += 1
        (OUT / f'{fid}.css').write_text('\n'.join(out) + '\n', encoding='utf-8')
        print(f'{fid}: {n} файлов')
    print(f'всего {total // 1024} КБ -> {OUT}')


if __name__ == '__main__':
    main()
