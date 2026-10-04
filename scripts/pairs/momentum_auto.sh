#!/usr/bin/env bash
# Проверка вперёд 0023: портфель месяца по расписанию — для таймера на сервере (deploy/momentum-auto.timer).
#
# Идемпотентен: если портфель текущего месяца уже в журнале — выходит. Поэтому запускается несколько
# раз в день: пропуск одного запуска не страшен, а первый успешный в новом месяце считает портфель
# по закрытому прошлому месяцу и коммитит журнал до клиринга первого торгового дня (~18:50 МСК).
# Пуш на origin — внешняя метка времени: портфель выбран до исхода.
#
# Лог: data/momentum-auto.log. Последняя строка «ГОТОВО» / «ОШИБКА» — состояние.
set -uo pipefail
cd "$(dirname "$0")/../.."
mkdir -p data
LOG=data/momentum-auto.log
exec >>"$LOG" 2>&1

MONTH=$(TZ=Europe/Moscow date +%Y-%m)
JOURNAL=docs/analysis/0023-journal.csv
echo "=== $(TZ=Europe/Moscow date -Is) месяц $MONTH"

has_month() { grep -q "^$MONTH," "$JOURNAL" 2>/dev/null; }

if has_month; then
    echo "портфель $MONTH уже в журнале — ничего не делаю"
    exit 0
fi
git pull --ff-only || echo "git pull не прошёл — считаю на локальной копии"
if has_month; then
    echo "портфель $MONTH пришёл с origin — ничего не делаю"
    exit 0
fi

fail() {
    echo "ОШИБКА: $1"
    ./scripts/notify.sh "Моментум 0023: портфель $MONTH НЕ записан — $1. Следующая попытка по таймеру; записать надо до клиринга первого торгового дня (~18:50 МСК). Лог: ~/momentum-paper/$LOG"
    exit 1
}
make momentum-update || fail "догрузка ISS"
make momentum-signal MONTH="$MONTH" || fail "расчёт портфеля"
git add "$JOURNAL" || fail "git add"
git commit -m "data(0023): портфель $MONTH (авто, $(hostname))" || fail "git commit"
pushed="и запушен"
git push || { pushed="но НЕ запушен — метки времени снаружи нет"; echo "ВНИМАНИЕ: пуш не прошёл — коммит только локальный"; }
make momentum-score || echo "ВНИМАНИЕ: score не посчитался"
summary=$(awk -F, -v m="$MONTH" '$1==m {s[$5]=s[$5] " " $4} END {printf "лонг:%s; шорт:%s", s["лонг"], s["шорт"]}' "$JOURNAL")
./scripts/notify.sh "Моментум 0023: портфель $MONTH записан $pushed. $summary"
echo "ГОТОВО: портфель $MONTH в журнале"
