#!/usr/bin/env bash
# Бот и проверка 0023 под общим пользователем apps — запуск от root на сервере.
#
# apps — один не-root пользователь для всех своих проектов на сервере: у каждого своя папка в /home/apps
# и своя служба (телеграм-мониторинг ставит свой deploy/setup-server.sh в том же apps). До 2026-10-05
# пользователь назывался trader — скрипт переименует его сам (шаг 0).
#
#   cd /root/projects/bcs-trading-bot && git fetch origin research/pref-common-pairs
#   git show origin/research/pref-common-pairs:deploy/setup-apps.sh > /root/setup-apps.sh
#   bash /root/setup-apps.sh
#
# Повторный запуск безопасен: каждый шаг проверяет, сделан ли он. Старая копия /root/projects/bcs-trading-bot
# не трогается (откат: systemctl disable --now trading-bot; cd /root/projects/bcs-trading-bot && make bot).
#
# Что получится:
#   apps — без пароля, вход по тем же ssh-ключам, что у root; без sudo, кроме управления своими службами;
#   /home/apps/bcs-trading-bot — бот (main), служба trading-bot.service;
#   /home/apps/momentum-paper — проверка 0023, таймер momentum-auto.timer;
#   /etc/trading-bot/env — секреты (root:apps 640) вместо export руками;
#   GitHub — deploy key только на этот репозиторий (ключ печатается в конце, добавить в настройках репо);
#   Telegram — сбои бота и ежедневной репетиции 0023 (TG_BOT_TOKEN, TG_CHAT_ID в /etc/trading-bot/env).
set -euo pipefail

U=apps
H=/home/$U
OLD=${OLD:-/root/projects/bcs-trading-bot}
REPO=git@github.com:motokazmin/bcs-trading-bot.git
MOM_BRANCH=research/pref-common-pairs
ENVF=/etc/trading-bot/env
LOGD=/var/log/trading-bot
BOT=$H/bcs-trading-bot
MOM=$H/momentum-paper

step() { echo; echo "== $*"; }
as_u() { sudo -u "$U" -H bash -lc "$1"; }

[ "$(id -u)" = 0 ] || { echo "запускать от root"; exit 1; }
[ -x /usr/local/go/bin/go ] || { echo "нет Go в /usr/local/go"; exit 1; }
for c in git make sqlite3 rsync python3 curl; do command -v $c >/dev/null || { echo "нет $c"; exit 1; }; done

step "0. trader → $U (общий пользователь вместо пользователя на проект)"
if id trader >/dev/null 2>&1 && ! id "$U" >/dev/null 2>&1; then
    # usermod -l не переименует пользователя с живыми процессами
    systemctl stop momentum-auto.timer momentum-health.timer trading-bot 2>/dev/null || true
    pkill -u trader || true
    sleep 2
    usermod -l "$U" trader
    groupmod -n "$U" trader
    usermod -d "$H" -m "$U"
    rm -f /etc/sudoers.d/trader /etc/sudoers.d/apps
    echo "trader переименован в $U, дом перенесён в $H (ключи, репозитории, история фьючерсов — вместе с ним)"
else
    echo "не требуется"
fi

step "1. Пользователь $U"
id "$U" >/dev/null 2>&1 || adduser --disabled-password --gecos "" "$U"
passwd -l "$U" >/dev/null
usermod -aG systemd-journal "$U"   # journalctl по службам без sudo
install -d -m 700 -o "$U" -g "$U" "$H/.ssh"
# Входить в apps могут те же ключи, что и в root: источник правды — /root/.ssh/authorized_keys.
install -m 600 -o "$U" -g "$U" /root/.ssh/authorized_keys "$H/.ssh/authorized_keys"

step "2. Секреты → $ENVF"
install -d -m 750 -o root -g "$U" /etc/trading-bot
if [ ! -s "$ENVF" ]; then
    PID=$(pgrep -f "bin/bot -config" | head -1 || true)
    [ -n "$PID" ] || { echo "бот не запущен: создайте $ENVF руками (BCS_REFRESH_TOKEN=…, ADMIN_TOKEN=…, HTTP_LISTEN=…)"; exit 1; }
    { echo "# Окружение бота: systemd EnvironmentFile и логин-шелл apps. Создан из работающего бота $(date -I)."
      tr '\0' '\n' < "/proc/$PID/environ" | grep -E '^(BCS_REFRESH_TOKEN|ADMIN_TOKEN|HTTP_LISTEN)='; } > "$ENVF"
fi
grep -q '^HTTP_LISTEN=' "$ENVF" || echo 'HTTP_LISTEN=0.0.0.0:8091' >> "$ENVF"
grep -q '^BCS_REFRESH_TOKEN=.' "$ENVF" || { echo "в $ENVF нет BCS_REFRESH_TOKEN"; exit 1; }
chown root:"$U" "$ENVF"; chmod 640 "$ENVF"
echo "переменные: $(grep -oE '^[A-Z_]+' "$ENVF" | tr '\n' ' ')"
if ! grep -q "$ENVF" "$H/.profile" 2>/dev/null; then
    cat >> "$H/.profile" <<EOF

# Переменные бота — из одного файла, без export руками
[ -r $ENVF ] && { set -a; . $ENVF; set +a; }
export PATH=\$PATH:/usr/local/go/bin:\$HOME/go/bin
EOF
    chown "$U:$U" "$H/.profile"
fi

step "3. Ключ GitHub для $U (deploy key)"
[ -f "$H/.ssh/id_ed25519" ] || as_u "ssh-keygen -q -t ed25519 -N '' -C '$U@$(hostname) deploy' -f ~/.ssh/id_ed25519"
as_u "ssh-keyscan -t ed25519 github.com 2>/dev/null >> ~/.ssh/known_hosts; sort -u -o ~/.ssh/known_hosts ~/.ssh/known_hosts"

step "4. Копии репозитория (клон ключом root, дальше — владелец $U)"
[ -d "$BOT/.git" ] || git clone -q "$REPO" "$BOT"
[ -d "$MOM/.git" ] || git clone -q -b "$MOM_BRANCH" "$REPO" "$MOM"
chown -R "$U:$U" "$BOT" "$MOM"
as_u "git -C $MOM config user.name momentum-bot && git -C $MOM config user.email motokazmin@users.noreply.github.com"

step "5. Сборка бота"
as_u "cd $BOT && make build-bot"

step "6. История фьючерсов для 0023"
if [ ! -s "$MOM/data/futures-d/contracts.csv" ]; then
    # ~35 минут, в фоне; таймер до ноября ничего не считает (портфель октября уже в журнале)
    as_u "cd $MOM && mkdir -p data && setsid nohup sh -c 'python3 scripts/pairs/futures_iss.py discover && python3 scripts/pairs/futures_iss.py fetch && PAIRS_END=\$(date +%F) python3 scripts/pairs/futures_series.py' > data/futures-initial.log 2>&1 &"
    echo "загрузка запущена в фоне: tail $MOM/data/futures-initial.log"
else
    echo "уже есть"
fi

step "7. Перенос бота: остановка старого, БД онлайн-бэкапом, данные"
if systemctl is-enabled -q trading-bot 2>/dev/null; then
    echo "служба trading-bot уже установлена — перенос был, пропускаю"
else
    PID=$(pgrep -f "bin/bot -config" | head -1 || true)
    if [ -n "$PID" ]; then
        kill -TERM "$PID"
        for _ in $(seq 60); do kill -0 "$PID" 2>/dev/null || break; sleep 1; done
        kill -0 "$PID" 2>/dev/null && { echo "старый бот не остановился за 60 с"; exit 1; }
        echo "старый бот остановлен"
    fi
    rsync -a --exclude 'trades.db*' --exclude bot.pid "$OLD/data/" "$BOT/data/"
    rm -f "$BOT/data/trades.db-wal" "$BOT/data/trades.db-shm"
    # WAL: копировать только онлайн-бэкапом (CLAUDE.md), иначе состояние на последний чекпоинт
    sqlite3 "$OLD/data/trades.db" ".backup '$BOT/data/trades.db'"
    chown -R "$U:$U" "$BOT/data"
    [ "$(sqlite3 "$BOT/data/trades.db" 'pragma integrity_check')" = ok ] || { echo "БД повреждена при переносе"; exit 1; }
    for t in closed_trades rejected_signals; do
        a=$(sqlite3 "$OLD/data/trades.db" "select count(*) from $t" 2>/dev/null || echo -)
        b=$(sqlite3 "$BOT/data/trades.db" "select count(*) from $t" 2>/dev/null || echo -)
        echo "$t: было $a, стало $b"
        [ "$a" = "$b" ] || { echo "расходится число строк — стоп"; exit 1; }
    done
fi
install -d -o "$U" -g "$U" "$LOGD"
chown -R "$U:$U" "$LOGD"

step "8. Службы systemd"
install -m 644 "$MOM/deploy/trading-bot.service" /etc/systemd/system/trading-bot.service
install -m 644 "$MOM/deploy/momentum-auto.service" /etc/systemd/system/momentum-auto.service
install -m 644 "$MOM/deploy/momentum-auto.timer" /etc/systemd/system/momentum-auto.timer
install -m 644 "$MOM/deploy/momentum-health.service" /etc/systemd/system/momentum-health.service
install -m 644 "$MOM/deploy/momentum-health.timer" /etc/systemd/system/momentum-health.timer
install -m 644 "$MOM/deploy/notify@.service" "/etc/systemd/system/notify@.service"
systemctl daemon-reload
systemctl enable --now trading-bot
systemctl enable --now momentum-auto.timer momentum-health.timer

step "9. sudo для $U — только эти службы"
cat > /etc/sudoers.d/$U-trading-bot <<EOF
# apps: службы бота и проверки 0023 (deploy/setup-apps.sh); у других проектов — свои файлы apps-*
$U ALL=(root) NOPASSWD: /usr/bin/systemctl start trading-bot, /usr/bin/systemctl stop trading-bot, /usr/bin/systemctl restart trading-bot, /usr/bin/systemctl start momentum-auto.service
EOF
chmod 440 /etc/sudoers.d/$U-trading-bot
visudo -cf /etc/sudoers.d/$U-trading-bot >/dev/null || { rm -f /etc/sudoers.d/$U-trading-bot; echo "sudoers не прошёл проверку — удалён"; exit 1; }

step "10. Проверка"
sleep 8
echo "trading-bot: $(systemctl is-active trading-bot)"
PORT=$(grep -oE '^HTTP_LISTEN=.*:([0-9]+)' "$ENVF" | grep -oE '[0-9]+$' || echo 8091)
echo "админка 127.0.0.1:$PORT → HTTP $(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/" || echo нет)"
tail -3 "$LOGD/bot.log" || true
systemctl list-timers momentum-auto.timer --no-pager | head -3
if as_u "ssh -o BatchMode=yes -T git@github.com" 2>&1 | grep -q "successfully authenticated"; then
    echo "GitHub: ключ $U работает"
else
    echo
    echo "!! GitHub: добавьте ключ $U как deploy key С ПРАВОМ ЗАПИСИ:"
    echo "   github.com/motokazmin/bcs-trading-bot → Settings → Deploy keys → Add deploy key → Allow write access"
    echo
    cat "$H/.ssh/id_ed25519.pub"
fi
if grep -q '^TG_BOT_TOKEN=.' "$ENVF" && grep -q '^TG_CHAT_ID=.' "$ENVF"; then
    as_u "$MOM/scripts/notify.sh 'setup-apps: уведомления работают'" && echo "Telegram: тестовое сообщение отправлено"
else
    echo
    echo "!! Telegram не настроен: допишите в $ENVF строки TG_BOT_TOKEN=… и TG_CHAT_ID=… (docs/runbook.md)"
fi
echo
echo "ГОТОВО. Вход: ssh $U@$(hostname -I | awk '{print $1}'). Старая копия $OLD не тронута."
