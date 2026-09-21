#!/usr/bin/env bash
# Служба, раз в час пересобирающая версию для GitHub Pages и выкладывающая её.
# Отдельно от основных: пишет наружу, в чужой репозиторий, от вашего имени —
# такое включают осознанно.
set -euo pipefail
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOME_DIR="$HOME/.schedulefu"; LOG_DIR="$HOME_DIR/logs"; AGENTS_DIR="$HOME/Library/LaunchAgents"
NAME="pro.schedulefu.pages"; INTERVAL="${INTERVAL:-3600}"
mkdir -p "$LOG_DIR" "$AGENTS_DIR"
cat > "$HOME_DIR/deploy-pages.sh" <<INNER
#!/usr/bin/env bash
set -euo pipefail
cd "$REPO_DIR"
# launchd запускает с урезанным окружением: без PATH не найдутся go и git.
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
export SCHEDULEFU_CONFIG="$HOME_DIR/config.yaml"
exec ./scripts/deploy-pages.sh
INNER
chmod +x "$HOME_DIR/deploy-pages.sh"
cat > "$AGENTS_DIR/$NAME.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$NAME</string>
  <key>ProgramArguments</key><array><string>$HOME_DIR/deploy-pages.sh</string></array>
  <key>StartInterval</key><integer>$INTERVAL</integer>
  <key>RunAtLoad</key><false/>
  <key>StandardOutPath</key><string>$LOG_DIR/$NAME.log</string>
  <key>StandardErrorPath</key><string>$LOG_DIR/$NAME.log</string>
</dict></plist>
PLIST
launchctl unload "$AGENTS_DIR/$NAME.plist" 2>/dev/null || true
launchctl load "$AGENTS_DIR/$NAME.plist"
echo "Выкладка на Pages: раз в $((INTERVAL/60)) мин. Вручную: $HOME_DIR/deploy-pages.sh"
