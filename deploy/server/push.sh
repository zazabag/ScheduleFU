#!/usr/bin/env bash
# Выкатка на сервер с Мака: собрать бинарник под Linux, отправить его,
# справочники и скрипты, запустить установку. Идемпотентно — так же
# выкатывается и каждое обновление.
#
#   deploy/server/push.sh                # сервер и домен по умолчанию
#   HOST=1.2.3.4 DOMAIN=sched.example deploy/server/push.sh
#
# Вход по ключу ~/.ssh/schedulefu_deploy; если ключа на сервере ещё нет —
# сначала deploy/server/bootstrap.sh.
set -euo pipefail

HOST="${HOST:-31.76.6.36}"
DOMAIN="${DOMAIN:-31-76-6-36.sslip.io}"
KEY="${KEY:-$HOME/.ssh/schedulefu_deploy}"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SSH="ssh -i $KEY -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new root@$HOST"
SCP="scp -i $KEY -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new"

echo "==> Сборка под Linux"
BIN="$(mktemp -d)/schedulefu"
(cd "$REPO_DIR" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$BIN" ./cmd/schedulefu)

echo "==> Отправка на $HOST"
$SSH 'mkdir -p /opt/schedulefu/data /opt/schedulefu/deploy'
$SCP "$BIN" root@$HOST:/opt/schedulefu/schedulefu.new
$SCP "$REPO_DIR"/data/*.json root@$HOST:/opt/schedulefu/data/
$SCP "$REPO_DIR"/deploy/server/install.sh "$REPO_DIR"/deploy/server/harden.sh root@$HOST:/opt/schedulefu/deploy/
rm -rf "$(dirname "$BIN")"

echo "==> Установка"
$SSH "DOMAIN=$DOMAIN bash /opt/schedulefu/deploy/install.sh"

echo "==> Проверка"
sleep 5
curl -fsS -m 20 "https://$DOMAIN/api/v1/health" && echo || echo "health пока не отвечает: сертификат может выпускаться минуту-две"
