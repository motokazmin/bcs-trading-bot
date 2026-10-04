#!/usr/bin/env bash
# Сообщение в Telegram: scripts/notify.sh "текст".
# Токен бота и чат — TG_BOT_TOKEN, TG_CHAT_ID в /etc/trading-bot/env (или в окружении).
# Telegram не настроен — пишет в stderr и выходит с 0: отсутствие канала не должно ронять вызывающий скрипт.
set -uo pipefail
ENVF=/etc/trading-bot/env
if [ -z "${TG_BOT_TOKEN:-}" ] && [ -r "$ENVF" ]; then set -a; . "$ENVF"; set +a; fi
MSG="[$(hostname -s)] $*"
if [ -z "${TG_BOT_TOKEN:-}" ] || [ -z "${TG_CHAT_ID:-}" ]; then
    echo "notify (Telegram не настроен): $MSG" >&2
    exit 0
fi
code=$(curl -sS -m 20 --retry 3 -o /dev/null -w '%{http_code}' \
    "https://api.telegram.org/bot${TG_BOT_TOKEN}/sendMessage" \
    --data-urlencode "chat_id=${TG_CHAT_ID}" --data-urlencode "text=${MSG}")
[ "$code" = 200 ] || { echo "notify: Telegram ответил $code" >&2; exit 1; }
