#!/usr/bin/env bash
# Выкладка статической версии на GitHub Pages.
#
# Собирает сайт из текущей базы и публикует его в ветку gh-pages.
# Ветка сиротская: в ней лежит только сайт, без исходников и истории
# разработки — так Pages отдаёт страницы из корня, а история основной
# ветки не засоряется выгрузками данных.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${BUILD_DIR:-/tmp/schedulefu-pages}"
REMOTE="${REMOTE:-https://github.com/zazabag/ScheduleFU.git}"

cd "$REPO_DIR"

echo "Собираю сайт…"
rm -rf "$BUILD_DIR"
go run ./cmd/staticgen -out "$BUILD_DIR"

echo "Публикую в gh-pages…"
cd "$BUILD_DIR"
git init -q
git checkout -q -b gh-pages
git add -A
git -c user.email="$(git -C "$REPO_DIR" config user.email || echo noreply@example.com)" \
    -c user.name="$(git -C "$REPO_DIR" config user.name || echo ScheduleFU)" \
    commit -q -m "Выгрузка расписания $(date +%Y-%m-%d\ %H:%M)"
git push -q --force "$REMOTE" gh-pages

echo "Готово: https://zazabag.github.io/ScheduleFU/"
