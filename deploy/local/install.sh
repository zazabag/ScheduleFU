#!/usr/bin/env bash
# Установка ScheduleFU как локальных служб на macOS.
#
# Один бинарник, две службы launchd под пользователем, без sudo: сборщик
# (раз в час обновляет расписание) и сервер (страницы и уведомления).
# Настройки — ~/.schedulefu/config.yaml, секреты — ~/.schedulefu/env.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOME_DIR="$HOME/.schedulefu"
BIN="$HOME_DIR/bin/schedulefu"
LOG_DIR="$HOME_DIR/logs"
AGENTS_DIR="$HOME/Library/LaunchAgents"
DB_NAME="${DB_NAME:-schedulefu}"
PORT="${PORT:-8090}"

mkdir -p "$HOME_DIR/bin" "$LOG_DIR" "$AGENTS_DIR"

echo "==> Собираю бинарник"
(cd "$REPO_DIR" && go build -o "$BIN" ./cmd/schedulefu)

echo "==> База"
if ! psql -lqt 2>/dev/null | cut -d'|' -f1 | grep -qw "$DB_NAME"; then
  createdb "$DB_NAME"; echo "    создана $DB_NAME"
else
  echo "    $DB_NAME уже есть"
fi

if [ ! -f "$HOME_DIR/config.yaml" ]; then
  cat > "$HOME_DIR/config.yaml" <<YAML
stand:
  env: dev
http:
  addr: ":$PORT"
db:
  dsn: postgres://localhost:5432/$DB_NAME?sslmode=disable
source:
  university: fa
  base_url: https://ruz.fa.ru
  interval: 1h
  days: 7
static:
  out_dir: /tmp/schedulefu-pages
YAML
  echo "    конфиг: $HOME_DIR/config.yaml"
fi

if [ ! -f "$HOME_DIR/env" ]; then
  echo "==> Ключи уведомлений"
  "$BIN" vapid 2>/dev/null > "$HOME_DIR/env"
  chmod 600 "$HOME_DIR/env"   # секрет: читать может только владелец
elif grep -q '^VAPID_PUBLIC_KEY=' "$HOME_DIR/env"; then
  # Файл от первой версии: имена переменных другие, а ключи те же. Менять
  # ключи нельзя — это обнулило бы подписки; переименовываем переменные.
  echo "==> Перевожу ключи на новые имена переменных"
  sed -i '' -e 's/^VAPID_PUBLIC_KEY=/SCHEDULEFU_NOTIFY_VAPID_PUBLIC=/' \
            -e 's/^VAPID_PRIVATE_KEY=/SCHEDULEFU_NOTIFY_VAPID_PRIVATE=/' \
            -e '/^DATABASE_URL=/d' -e '/^ADDR=/d' "$HOME_DIR/env"
fi

export SCHEDULEFU_CONFIG="$HOME_DIR/config.yaml"
set -a; . "$HOME_DIR/env"; set +a

echo "==> Миграции и справочники"
"$BIN" migrate
(cd "$REPO_DIR" && "$BIN" seed -data data)

echo "==> Первый сбор (без уведомлений: всё расписание — не новость)"
"$BIN" collect -once -no-notify

write_agent() {
  local name="$1"; shift
  local plist="$AGENTS_DIR/$name.plist"
  {
    echo '<?xml version="1.0" encoding="UTF-8"?>'
    echo '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">'
    echo '<plist version="1.0"><dict>'
    echo "  <key>Label</key><string>$name</string>"
    echo '  <key>ProgramArguments</key><array>'
    echo "    <string>$BIN</string>"
    for a in "$@"; do echo "    <string>$a</string>"; done
    echo '  </array>'
    echo '  <key>EnvironmentVariables</key><dict>'
    echo "    <key>SCHEDULEFU_CONFIG</key><string>$HOME_DIR/config.yaml</string>"
    while IFS='=' read -r k v; do
      [ -z "$k" ] && continue; case "$k" in \#*) continue;; esac
      echo "    <key>$k</key><string>$v</string>"
    done < "$HOME_DIR/env"
    echo '  </dict>'
    echo '  <key>RunAtLoad</key><true/>'
    # Поднимать только после аварии; остановленная руками остаётся стоять.
    echo '  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>'
    echo "  <key>StandardOutPath</key><string>$LOG_DIR/$name.log</string>"
    echo "  <key>StandardErrorPath</key><string>$LOG_DIR/$name.log</string>"
    echo "  <key>WorkingDirectory</key><string>$HOME_DIR</string>"
    echo '</dict></plist>'
  } > "$plist"
  launchctl unload "$plist" 2>/dev/null || true
  launchctl load "$plist"
  echo "    служба $name запущена"
}

echo "==> Службы"
write_agent pro.schedulefu.api serve
write_agent pro.schedulefu.collector collect

echo
echo "Готово. Открыть: http://localhost:$PORT"
echo "Логи: $LOG_DIR · остановить: $REPO_DIR/deploy/local/uninstall.sh"
