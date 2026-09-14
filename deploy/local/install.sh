#!/usr/bin/env bash
# Установка ScheduleFU как локальной службы на macOS.
#
# Две службы launchd: сборщик, который раз в час обновляет расписание, и
# сервер, который отдаёт страницы и рассылает уведомления. Обе живут под
# пользователем, без sudo и без прав администратора.
#
# Ключи уведомлений и настройки хранятся в ~/.schedulefu/env — вне
# репозитория: приватному ключу в git не место.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOME_DIR="$HOME/.schedulefu"
BIN_DIR="$HOME_DIR/bin"
LOG_DIR="$HOME_DIR/logs"
AGENTS_DIR="$HOME/Library/LaunchAgents"
DB_NAME="${DB_NAME:-schedulefu}"
PORT="${PORT:-8090}"

mkdir -p "$BIN_DIR" "$LOG_DIR" "$AGENTS_DIR"

echo "==> Собираю бинарники"
cd "$REPO_DIR"
go build -o "$BIN_DIR/schedulefu-api" ./cmd/api
go build -o "$BIN_DIR/schedulefu-collector" ./cmd/collector
go build -o "$BIN_DIR/schedulefu-vapid" ./cmd/vapid

echo "==> Проверяю базу"
if ! psql -lqt 2>/dev/null | cut -d'|' -f1 | grep -qw "$DB_NAME"; then
  createdb "$DB_NAME"
  echo "    создана база $DB_NAME"
else
  echo "    база $DB_NAME уже есть"
fi

if [ ! -f "$HOME_DIR/env" ]; then
  echo "==> Создаю ключи уведомлений"
  {
    echo "DATABASE_URL=postgres://localhost:5432/$DB_NAME?sslmode=disable"
    echo "ADDR=:$PORT"
    "$BIN_DIR/schedulefu-vapid" 2>/dev/null
  } > "$HOME_DIR/env"
  # Ключ уведомлений — секрет: читать может только владелец.
  chmod 600 "$HOME_DIR/env"
  echo "    ключи в $HOME_DIR/env"
else
  echo "==> Ключи уже есть, оставляю как были"
fi

# shellcheck disable=SC1090
set -a; . "$HOME_DIR/env"; set +a

echo "==> Наполняю справочники"
"$BIN_DIR/schedulefu-collector" -seed -dsn "$DATABASE_URL"

echo "==> Первый сбор расписания (без уведомлений: всё расписание — не новость)"
"$BIN_DIR/schedulefu-collector" -once -days 7 -no-notify -dsn "$DATABASE_URL"

write_agent() {
  local name="$1" program="$2"; shift 2
  local args=("$@")
  local plist="$AGENTS_DIR/$name.plist"
  {
    echo '<?xml version="1.0" encoding="UTF-8"?>'
    echo '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">'
    echo '<plist version="1.0"><dict>'
    echo "  <key>Label</key><string>$name</string>"
    echo '  <key>ProgramArguments</key><array>'
    echo "    <string>$program</string>"
    for a in "${args[@]}"; do echo "    <string>$a</string>"; done
    echo '  </array>'
    echo '  <key>EnvironmentVariables</key><dict>'
    while IFS='=' read -r key value; do
      [ -z "$key" ] && continue
      case "$key" in \#*) continue;; esac
      echo "    <key>$key</key><string>$value</string>"
    done < "$HOME_DIR/env"
    echo '  </dict>'
    echo '  <key>RunAtLoad</key><true/>'
    # KeepAlive только при ненулевом выходе: служба, упавшая по ошибке,
    # поднимется сама, а остановленная вручную — останется остановленной.
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

echo "==> Ставлю службы"
write_agent "pro.schedulefu.api" "$BIN_DIR/schedulefu-api"
# Сборщик работает в цикле сам: интервал задаётся ему, а не launchd —
# так он переживает ночь одним процессом и не пересоздаёт соединения.
write_agent "pro.schedulefu.collector" "$BIN_DIR/schedulefu-collector" \
  "-interval" "1h" "-days" "7"

echo
echo "Готово. Открыть: http://localhost:$PORT"
echo "Логи:     $LOG_DIR"
echo "Остановить: $REPO_DIR/deploy/local/uninstall.sh"
