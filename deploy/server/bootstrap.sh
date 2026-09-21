#!/usr/bin/env bash
# Первый и единственный шаг, который требует человека: положить ключ на
# новый сервер. Хостер не даёт добавить ключ через панель, поэтому один
# раз вводится пароль root из письма хостера. Дальше всё делает push.sh,
# а harden.sh закрывает вход по паролю насовсем.
set -euo pipefail

HOST="${HOST:-31.76.6.36}"
KEY="${KEY:-$HOME/.ssh/schedulefu_deploy}"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

[ -f "$KEY" ] || ssh-keygen -t ed25519 -N '' -C schedulefu-deploy -f "$KEY"

echo "==> Ключ на сервер (спросит пароль root — один раз)"
ssh-copy-id -i "$KEY.pub" -o StrictHostKeyChecking=accept-new "root@$HOST"

echo "==> Выкатка"
HOST="$HOST" KEY="$KEY" "$DIR/push.sh"

echo "==> Закрываю вход по паролю"
ssh -i "$KEY" -o IdentitiesOnly=yes "root@$HOST" 'bash /opt/schedulefu/deploy/harden.sh'
