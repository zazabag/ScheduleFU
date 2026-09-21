#!/usr/bin/env bash
# Закрыть вход по паролю после того, как ключ на месте.
# Отдельно от install.sh намеренно: если запустить это раньше, чем ключ
# проверен, сервер станет недоступен насовсем.
set -euo pipefail
[ -s /root/.ssh/authorized_keys ] || { echo "в /root/.ssh/authorized_keys нет ключей — не закрываю"; exit 1; }
cat > /etc/ssh/sshd_config.d/00-schedulefu.conf <<CONF
PasswordAuthentication no
PermitRootLogin prohibit-password
KbdInteractiveAuthentication no
CONF
sshd -t && systemctl reload ssh
echo "вход по паролю закрыт; root — только по ключу"
