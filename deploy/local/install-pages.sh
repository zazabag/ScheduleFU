#!/usr/bin/env bash
# Служба, которая раз в час пересобирает статическую версию и выкладывает
# её на GitHub Pages.
#
# Ставится отдельно от основных служб, потому что делает то, чего они не
# делают: пишет наружу, в чужой репозиторий, от вашего имени. Такое
# включают осознанно.
#
# История gh-pages не растёт: выкладка перезаписывает ветку целиком, иначе
# ежечасные выгрузки данных за месяц превратили бы репозиторий в свалку.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOME_DIR="$HOME/.schedulefu"
LOG_DIR="$HOME_DIR/logs"
AGENTS_DIR="$HOME/Library/LaunchAgents"
NAME="pro.schedulefu.pages"
INTERVAL="${INTERVAL:-3600}"

mkdir -p "$LOG_DIR" "$AGENTS_DIR"

cat > "$HOME_DIR/deploy-pages.sh" <<INNER
#!/usr/bin/env bash
set -euo pipefail
cd "$REPO_DIR"
# PATH задаётся явно: launchd запускает службы с урезанным окружением,
# и без этого не находятся ни go, ни git.
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
export DATABASE_URL="postgres://localhost:5432/${DB_NAME:-schedulefu}?sslmode=disable"
exec ./scripts/deploy-pages.sh
INNER
chmod +x "$HOME_DIR/deploy-pages.sh"

cat > "$AGENTS_DIR/$NAME.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$NAME</string>
  <key>ProgramArguments</key><array>
    <string>$HOME_DIR/deploy-pages.sh</string>
  </array>
  <key>StartInterval</key><integer>$INTERVAL</integer>
  <key>RunAtLoad</key><false/>
  <key>StandardOutPath</key><string>$LOG_DIR/$NAME.log</string>
  <key>StandardErrorPath</key><string>$LOG_DIR/$NAME.log</string>
</dict></plist>
PLIST

launchctl unload "$AGENTS_DIR/$NAME.plist" 2>/dev/null || true
launchctl load "$AGENTS_DIR/$NAME.plist"

echo "Служба выкладки установлена: раз в $((INTERVAL / 60)) мин."
echo "Проверить вручную: $HOME_DIR/deploy-pages.sh"
echo "Логи: $LOG_DIR/$NAME.log"
