#!/usr/bin/env bash
# Установка ScheduleFU на Ubuntu 24.04. Запускается на сервере от root,
# идемпотентна: повторный прогон обновляет бинарник и конфиг, не трогая
# базу и ключи.
#
#   DOMAIN=fa.planovo.pro REDIRECT_FROM=31-76-6-36.sslip.io bash install.sh
#
# Что ставится и почему:
#   PostgreSQL 16   — база слепков; из репозитория Ubuntu, версия та же, что локально
#   Caddy           — обратный прокси, сам получает и продлевает сертификат:
#                     без HTTPS не работают ни уведомления, ни установка на телефон
#   systemd-юниты   — serve, collect и notes; перезапуск при падении, лог в journald
#   ufw + fail2ban  — открыты только 22/80/443; пароль root перебирают круглосуточно
#   pg_dump         — раз в сутки, хранится 14 дней; терять нечего, кроме подписок
#
# Домен: пока своего нет, sslip.io отдаёт имя вида 31-76-6-36.sslip.io,
# которое резолвится в этот IP, и Let's Encrypt выдаёт на него сертификат.
# Свой домен потом — одной строкой в Caddyfile.
set -euo pipefail

DOMAIN="${DOMAIN:?задайте DOMAIN=имя.сайта}"
APP_USER=schedulefu
APP_DIR=/opt/schedulefu
DB_NAME=schedulefu
BIN_SRC="${BIN_SRC:-$APP_DIR/schedulefu.new}"

echo "==> Пакеты"
export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -y -q postgresql-16 ufw fail2ban curl ca-certificates gnupg >/dev/null

if ! command -v caddy >/dev/null; then
  echo "==> Caddy (официальный репозиторий)"
  curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/gpg.key | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt > /etc/apt/sources.list.d/caddy-stable.list
  apt-get update -q && apt-get install -y -q caddy >/dev/null
fi

echo "==> Пользователь и каталоги"
id -u $APP_USER >/dev/null 2>&1 || useradd --system --home $APP_DIR --shell /usr/sbin/nologin $APP_USER
mkdir -p $APP_DIR/backups
chown -R $APP_USER:$APP_USER $APP_DIR

echo "==> База"
sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='$APP_USER'" | grep -q 1 || sudo -u postgres createuser $APP_USER
sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='$DB_NAME'" | grep -q 1 || sudo -u postgres createdb -O $APP_USER $DB_NAME

echo "==> Бинарник"
if [ -f "$BIN_SRC" ]; then
  install -m 755 -o root -g root "$BIN_SRC" $APP_DIR/schedulefu && rm -f "$BIN_SRC"
fi
[ -x $APP_DIR/schedulefu ] || { echo "нет бинарника $APP_DIR/schedulefu"; exit 1; }

echo "==> Конфиг и ключи"
if [ ! -f $APP_DIR/config.yaml ]; then
  cat > $APP_DIR/config.yaml <<YAML
stand:
  env: prod
  origin: https://$DOMAIN
http:
  addr: "127.0.0.1:8090"
  trust_proxy: true
db:
  dsn: postgres:///$DB_NAME?host=/var/run/postgresql
source:
  university: fa
  base_url: https://ruz.fa.ru
  interval: 1h
  days: 7
notes:
  # Раздел «Пары»: запись занятия, конспект, домашние задания. Включать
  # после того, как положены модели распознавания и задан ключ модели
  # конспекта (SCHEDULEFU_NOTES_LLM_API_KEY в $APP_DIR/env) —
  # docs/07-notes-module.md § 8.
  enabled: false
  audio_dir: $APP_DIR/audio
  asr:
    model_dir: $APP_DIR/models/gigaam-v3
static:
  out_dir: /tmp/schedulefu-pages
  api_base: https://$DOMAIN
YAML
fi
if [ ! -f $APP_DIR/env ]; then
  # Ключи создаются один раз: смена обнуляет все подписки.
  sudo -u $APP_USER $APP_DIR/schedulefu vapid 2>/dev/null > $APP_DIR/env
  echo "SCHEDULEFU_NOTIFY_SUBJECT=mailto:admin@$DOMAIN" >> $APP_DIR/env
fi
chown $APP_USER:$APP_USER $APP_DIR/config.yaml $APP_DIR/env
chmod 600 $APP_DIR/env

echo "==> Службы"
install -d -o $APP_USER -g $APP_USER -m 700 $APP_DIR/audio $APP_DIR/models
for unit in serve collect notes; do
  cat > /etc/systemd/system/schedulefu-$unit.service <<UNIT
[Unit]
Description=ScheduleFU $unit
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
User=$APP_USER
WorkingDirectory=$APP_DIR
Environment=SCHEDULEFU_CONFIG=$APP_DIR/config.yaml
EnvironmentFile=$APP_DIR/env
ExecStart=$APP_DIR/schedulefu $unit
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$APP_DIR /tmp
PrivateTmp=true

[Install]
WantedBy=multi-user.target
UNIT
done
systemctl daemon-reload

echo "==> Миграции и первое наполнение"
sudo -u $APP_USER env SCHEDULEFU_CONFIG=$APP_DIR/config.yaml $APP_DIR/schedulefu migrate
if [ -f $APP_DIR/data/auditoriums.json ]; then
  sudo -u $APP_USER env SCHEDULEFU_CONFIG=$APP_DIR/config.yaml $APP_DIR/schedulefu seed -data $APP_DIR/data
fi
if [ "$(sudo -u postgres psql -tAc "SELECT count(*) FROM lessons" $DB_NAME)" = "0" ]; then
  sudo -u $APP_USER env SCHEDULEFU_CONFIG=$APP_DIR/config.yaml $APP_DIR/schedulefu collect -once -no-notify
fi

echo "==> Caddy"
cat > /etc/caddy/Caddyfile <<CADDY
$DOMAIN {
    # HSTS ставит прокси, а не приложение: приложение не знает, что снаружи TLS.
    header Strict-Transport-Security "max-age=31536000"
    encode zstd gzip
    reverse_proxy 127.0.0.1:8090
}
CADDY
# Прежние адреса не бросаем: у кого-то приложение установлено с них.
for old in ${REDIRECT_FROM:-}; do
  [ "$old" = "$DOMAIN" ] && continue
  cat >> /etc/caddy/Caddyfile <<CADDY

$old {
    redir https://$DOMAIN{uri} permanent
}
CADDY
done
systemctl enable -q --now caddy
systemctl reload caddy

echo "==> Файрвол и fail2ban"
ufw --force reset >/dev/null
ufw default deny incoming >/dev/null
ufw default allow outgoing >/dev/null
ufw allow 22/tcp >/dev/null && ufw allow 80/tcp >/dev/null && ufw allow 443/tcp >/dev/null
ufw --force enable >/dev/null
systemctl enable -q --now fail2ban

echo "==> Резервные копии"
cat > /etc/cron.daily/schedulefu-backup <<'CRON'
#!/bin/sh
sudo -u postgres pg_dump -Fc schedulefu > /opt/schedulefu/backups/$(date +%F).dump
find /opt/schedulefu/backups -name '*.dump' -mtime +14 -delete
CRON
chmod +x /etc/cron.daily/schedulefu-backup

echo "==> Запуск"
systemctl enable -q --now schedulefu-serve schedulefu-collect
systemctl restart schedulefu-serve schedulefu-collect
# Обработка записей включается вручную: без моделей она сразу выходит с
# ошибкой, и systemd крутил бы её по кругу.
#   systemctl enable --now schedulefu-notes
sleep 3
systemctl is-active schedulefu-serve schedulefu-collect caddy postgresql | tr '\n' ' '; echo
echo
echo "Готово: https://$DOMAIN"
