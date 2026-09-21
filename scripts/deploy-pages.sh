#!/usr/bin/env bash
# Сборка статической версии и выкладка в gh-pages.
# Ветка сиротская и перезаписывается целиком: история от ежечасных выгрузок
# не растёт.
set -euo pipefail
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${BUILD_DIR:-/tmp/schedulefu-pages}"
REMOTE="${REMOTE:-https://github.com/zazabag/ScheduleFU.git}"
cd "$REPO_DIR"
echo "Собираю сайт…"
rm -rf "$BUILD_DIR"
SCHEDULEFU_STATIC_OUT_DIR="$BUILD_DIR" go run ./cmd/schedulefu static
echo "Публикую в gh-pages…"
cd "$BUILD_DIR"
git init -q && git checkout -q -b gh-pages && git add -A
git -c user.email="$(git -C "$REPO_DIR" config user.email || echo noreply@example.com)" \
    -c user.name="$(git -C "$REPO_DIR" config user.name || echo ScheduleFU)" \
    commit -q -m "Выгрузка расписания $(date +%Y-%m-%d\ %H:%M)"
git push -q --force "$REMOTE" gh-pages
echo "Готово: https://zazabag.github.io/ScheduleFU/"
