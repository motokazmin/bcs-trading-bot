#!/usr/bin/env bash
# Ежедневная репетиция месячного расчёта 0023 (deploy/momentum-health.timer).
#
# Тревога в момент аварии бесполезна: портфель надо записать до клиринга первого торгового дня, и на ремонт
# останутся часы. Поэтому каждый день прогоняется весь путь вхолостую — догрузка ISS, свежесть данных,
# расчёт портфеля без записи, право пушить, место на диске — и о поломке известно за дни и недели.
# По понедельникам — «всё в порядке»: если сообщения нет, лежит сам сервер.
#
# Лог: data/momentum-health.log.
set -uo pipefail
cd "$(dirname "$0")/../.."
mkdir -p data
exec >>data/momentum-health.log 2>&1
echo "=== $(TZ=Europe/Moscow date -Is)"

problems=()
check() {
    local name=$1; shift
    if "$@"; then echo "ok: $name"; else echo "СБОЙ: $name"; problems+=("$name"); fi
}
fresh() {  # в склеенном ряду SBRF есть день не старше 6 календарных (выходные + праздник)
    python3 -c "
import pandas as pd, sys
d = pd.read_csv('data/futures-d/continuous/SBRF.csv', parse_dates=['date']).date.max()
print('данные по', d.date())
sys.exit(0 if (pd.Timestamp.today().normalize() - d).days <= 6 else 1)"
}

free_gb=$(df -BG --output=avail . | tail -1 | tr -dc 0-9)
check "место на диске ≥ 2 ГБ (свободно ${free_gb} ГБ)" test "$free_gb" -ge 2
check "git pull" git pull --ff-only -q
check "догрузка ISS" make -s momentum-update
check "данные ISS свежие" fresh
check "расчёт портфеля вхолостую" make -s momentum-signal-dry
check "право пушить на GitHub" git push --dry-run -q origin HEAD:refs/heads/__write_probe

month_end=$(TZ=Europe/Moscow date -d "$(date +%Y-%m-01) +1 month -1 day" +%d)
left=$(( 10#$month_end - 10#$(TZ=Europe/Moscow date +%d) ))
if [ ${#problems[@]} -gt 0 ]; then
    msg="Моментум 0023: сбой ежедневной проверки — ${problems[*]}. До конца месяца $left дн. Лог: ~/momentum-paper/data/momentum-health.log"
    echo "$msg"
    ./scripts/notify.sh "$msg"
    exit 1
fi
if [ "$(TZ=Europe/Moscow date +%u)" = 1 ]; then
    ./scripts/notify.sh "Еженедельно: всё в порядке. $(fresh 2>/dev/null | head -1), бот: $(systemctl is-active trading-bot 2>/dev/null || echo ?). До конца месяца $left дн."
fi
echo "ГОТОВО: проверка пройдена"
