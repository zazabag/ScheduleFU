#!/usr/bin/env bash
# Остановка и удаление локальных служб ScheduleFU.
# База и ключи остаются: снести их случайно хуже, чем оставить лишнее.
set -euo pipefail

AGENTS_DIR="$HOME/Library/LaunchAgents"
for name in pro.schedulefu.api pro.schedulefu.collector; do
  plist="$AGENTS_DIR/$name.plist"
  if [ -f "$plist" ]; then
    launchctl unload "$plist" 2>/dev/null || true
    rm -f "$plist"
    echo "служба $name остановлена и удалена"
  fi
done

echo
echo "База и ключи остались в ~/.schedulefu — удалите вручную, если нужно."
