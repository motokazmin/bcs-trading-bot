# Runbook: бот и optimizer

Шпаргалка запуска. Состояние слотов — [`analysis/state.md`](analysis/state.md), режимы optimizer — [`optimizer-modes.md`](optimizer-modes.md), CLI — [`cmd/optimizer/README.md`](../cmd/optimizer/README.md).

---

## Переменные окружения

| Переменная | Нужна для | Обязательность |
|---|---|---|
| `BCS_REFRESH_TOKEN` | Бот, `sync-history` | Да для бота и истории |
| `ADMIN_TOKEN` | HTTP-админка с публичного IP | Да при bind не на localhost |
| `HTTP_LISTEN` | Адрес `-http-listen` через `make bot` | Нет (дефолт `127.0.0.1:8091`) |
| `BOT_CONFIG` | YAML для `make bot` | Нет (дефолт `configs/runs/paper-m15.yaml`, только M15 с 2026-10-04) |
| `LOG_FILE` | Путь лог-файла | Нет (дефолт `/var/log/trading-bot/bot.log`; `-` — только stdout) |

```bash
export BCS_REFRESH_TOKEN="..."
# облако:
export ADMIN_TOKEN="$(openssl rand -hex 32)"
export HTTP_LISTEN=0.0.0.0:8091
# sudo mkdir -p /var/log/trading-bot && sudo chown "$USER" /var/log/trading-bot
```

Токены только в env, не в YAML.

---

## Бот: локально

```bash
make build
export BCS_REFRESH_TOKEN=...
make bot                 # фон; PID → data/bot.pid
# → http://127.0.0.1:8091
make bot-status
tail -f /var/log/trading-bot/bot.log
make bot-stop
```

| Команда | Что делает |
|---|---|
| `make bot` | Paper portfolio в **фоне**, 6 слотов |
| `make bot-stop` / `make bot-status` | Остановка / статус |
| `make bot-real` | Реал в фоне (осторожно; сейчас 1 experiment) |
| `make bot-smoke` | Smoke OAuth+WS (**foreground**) |
| `make bot-futures` | Paper фьючерсы в фоне |

---

## Бот: облако / VM

```bash
make build
export BCS_REFRESH_TOKEN=...
export ADMIN_TOKEN="$(openssl rand -hex 32)"
export HTTP_LISTEN=0.0.0.0:8091
make bot
# с ПК: http://PUBLIC_IP:8091 → ADMIN_TOKEN
# на VM:  make bot-status; tail -f /var/log/trading-bot/bot.log
# стоп:   make bot-stop
```

Без `ADMIN_TOKEN` процесс не стартует с `0.0.0.0`. Firewall: TCP **8091**.

## Выгрузка периода для разбора

**В админке:** страница «Экспорт для ИИ» → блок «Разбор — сделки и отклонённые
сигналы» → кнопка «Скачать incident.json». Фильтры сверху страницы применяются к
выгрузке, как и к остальным вариантам экспорта.

**Или запросом** — забрать сделки и отклонённые сигналы за период, не останавливая
бота и не копируя `data/trades.db`:

```bash
curl -s -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://ХОСТ:8091/api/export/incident?date_from=2026-09-08&date_to=2026-09-11" \
  > incident.json
```

Параметры те же, что у остальной аналитики: `date_from`, `date_to`, `experiment_id`,
`ticker`, `trading_mode`, `run_id`, `period` (явный выбор архива). Фильтр идёт через
`s.parseFilter`, поэтому архивные периоды скрыты так же, как в админке.

В ответе: `trades` (все поля `closed_trades`, включая `requested_quantity`,
`cash_at_open`, `bar_age_seconds`), `rejected_signals` и сводка `counts`
(`trades`, `cash_capped`, `rejected_signals`). Поле `truncated: true` означает, что
выборка упёрлась в потолок 50 000 строк и неполна — сузить период.

**Почему так, а не копированием файла.** База в режиме WAL: `scp` одного `trades.db`
без `trades.db-wal` отдаёт состояние на последний чекпоинт. Однажды это выглядело как
«сделок нет в базе» — локальная копия обрывалась на 2026-09-04 при сделках по 09-11 в
логе (разбор [`analysis/0002`](analysis/0002-live-config-drift-and-cash-sizing.md)).
Ручка отдаёт данные из открытого хендла работающего процесса, недокоммиченного
состояния для читателя не существует.

Если файл базы всё же нужен целиком — только онлайн-бэкапом, не `scp`:

```bash
sqlite3 ~/bcs-trading-bot/data/trades.db ".backup '/tmp/snap.db'"
```

Логи по умолчанию: `/var/log/trading-bot/bot.log`. `LOG_FILE=-` → stdout в `data/bot.stdout.log`. PID: `data/bot.pid` (`BOT_PID_FILE`).

---

## Запись сырого WS-потока

Бот пишет каждое WS-сообщение как есть в `data/ws-raw/<YYYY-MM-DD>.jsonl.gz`
(день по МСК, блок `recording` в конфиге; `enabled: false` — выключить). Строка:
`{"recv": <время получения, UTC>, "msg": <сообщение>}`. `"responseType":"_session"` —
маркер новой WS-сессии: между ним и предыдущим сообщением поток мог прерываться.
Каталог в git не идёт (`/data/*`), живёт на хосте — **забирать и бэкапить вручную**,
задним числом эти данные не восстановить.

```bash
# на хосте: пишется ли и сколько за день
ls -lh data/ws-raw/
zcat data/ws-raw/$(date +%F).jsonl.gz | wc -l
zcat data/ws-raw/$(date +%F).jsonl.gz | head -3
# в логе при закрытии дня / остановке:
grep wsrecord /var/log/trading-bot/bot.log | tail
```

**Всё ли в порядке — по логу** (VM с долей vCPU 10%, узкое место — открытие сессии):

| строка в логе | норма | тревога |
|---|---|---|
| `wsrecord: за 5m0s записано N ..., пик очереди Q/16384` (раз в 5 мин, пока идёт поток) | Q — единицы–десятки | Q в тысячах — CPU/диск не успевают; следующий шаг — потери |
| `wsrecord: отброшено сообщений за всё время: D` | строки нет | есть — запись неполная (торговля не тормозит) |
| `[OPEN] ... bar_age=Ns` у каждого входа | 0…10 с | больше — решения запаздывают, бар обрабатывается с задержкой |
| `[datafeed ...] бар ... закрыт по таймеру` | редко, в неликвиде/на закрытии | часто и на ликвидных — поток прерывается |
| `рыночные данные: ... переподключение` | единично | регулярно — связь или VM |

```bash
grep -E "wsrecord|bar_age|закрыт по таймеру|переподключение" /var/log/trading-bot/bot.log | tail -40
```

Лог не покажет steal CPU напрямую — если пик очереди растёт, смотреть `top` (поле `st`)
в 10:00–10:05.

## Сервер: общий пользователь apps, службы бота и моментума

Все свои проекты на сервере работают под **одним не-root пользователем `apps`**: у каждого своя папка в
`/home/apps`, своя служба systemd, свой файл sudoers (`/etc/sudoers.d/apps-<проект>`) и свой deploy key на GitHub
(ключ привязан к одному репозиторию — у apps их несколько, выбор по `~/.ssh/config`). Пользователь на проект —
зоопарк; root — всё под одним ударом. До 2026-10-05 пользователь назывался `trader`.

Бот и проверка [0023](analysis/0023-momentum-forward-paper.md) ставятся `deploy/setup-apps.sh` — один раз от root,
повторный запуск безопасен (и сам переименует `trader` → `apps`):

```bash
cd /root/projects/bcs-trading-bot && git fetch origin research/pref-common-pairs
git show origin/research/pref-common-pairs:deploy/setup-apps.sh > /root/setup-apps.sh
bash /root/setup-apps.sh
```

| Что | Где |
|---|---|
| бот | `/home/apps/bcs-trading-bot`, `trading-bot.service` (автозапуск, перезапуск при падении) |
| секреты | `/etc/trading-bot/env` (root:apps 640) — `BCS_REFRESH_TOKEN`, `ADMIN_TOKEN`, `HTTP_LISTEN`; логин-шелл apps подхватывает сам |
| моментум | `/home/apps/momentum-paper`, `momentum-auto.timer` (07/12/16 МСК) |
| GitHub | deploy key apps с правом записи — только этот репозиторий |
| sudo apps | только `systemctl start/stop/restart trading-bot`, `start momentum-auto.service` |

Скрипт останавливает старого бота (запущенного `make bot` от root), переносит `trades.db` онлайн-бэкапом и
сверяет число строк; старая копия `/root/projects/bcs-trading-bot` не трогается. Откат:
`systemctl disable --now trading-bot; cd /root/projects/bcs-trading-bot && make bot`.
В конце печатается публичный ключ apps — добавить в GitHub → Settings → Deploy keys, **Allow write access**.

Повседневное (под apps):

```bash
sudo systemctl restart trading-bot          # после git pull && make build-bot
systemctl status trading-bot; tail -f /var/log/trading-bot/bot.log
journalctl -u momentum-auto -n 30            # запуски таймера
tail data/momentum-auto.log                  # в ~/momentum-paper: «ГОТОВО …» / «ОШИБКА …»
make momentum-score
```

**Моментум по таймеру.** Раз в месяц надо посчитать портфель и закоммитить журнал **до клиринга первого
торгового дня** (~18:50 МСК). `scripts/pairs/momentum_auto.sh` 3 раза в день проверяет, есть ли портфель
текущего месяца в `docs/analysis/0023-journal.csv`; нет — догружает ISS, считает, коммитит и пушит. Есть — выходит.
`ВНИМАНИЕ: пуш не прошёл` — коммит только на сервере, внешней метки времени нет: проверить deploy key.
Вручную в любой момент: `sudo systemctl start momentum-auto.service` (лишний запуск ничего не сломает).

**Уведомления в Telegram.** Тревога в момент аварии бесполезна — портфель надо записать до клиринга, на ремонт
останутся часы. Поэтому `momentum-health.timer` **каждый день в 09:00 МСК** прогоняет месячный путь вхолостую
(догрузка ISS, свежесть данных, расчёт портфеля без записи, право пушить, место на диске) и пишет о сбое в тот же
день. По понедельникам — «всё в порядке»: **нет сообщения в понедельник — лежит сам сервер.** 1-го числа —
«портфель записан: лонг …, шорт …». Бот: 5 перезапусков за 10 минут — сообщение (`OnFailure=notify@`).

Настройка (один раз, от root): создать бота у @BotFather, написать ему любое сообщение, затем
```bash
read -rsp "токен бота: " T; echo; echo "TG_BOT_TOKEN=$T" >> /etc/trading-bot/env
curl -s "https://api.telegram.org/bot$T/getUpdates" | grep -o '"chat":{"id":-\?[0-9]*' | head -1 \
  | grep -o -- '-\?[0-9]*$' | sed 's/^/TG_CHAT_ID=/' >> /etc/trading-bot/env; unset T
grep -c '^TG_' /etc/trading-bot/env                       # 2
bash /root/setup-apps.sh                                # поставит таймер проверки и пришлёт тестовое сообщение
```

---

## Почему 127.0.0.1 vs 0.0.0.0

| Bind | Кто подключается | Сценарий |
|---|---|---|
| `127.0.0.1:8091` | Только эта машина | Ноутбук |
| `0.0.0.0:8091` | Любой интерфейс | VM + браузер с дома |

`make bot` подставляет `HTTP_LISTEN` (дефолт localhost). В облаке всегда `HTTP_LISTEN=0.0.0.0:8091`.

---

## Админка

Встроена в `cmd/bot`: `/` дашборд, `/open`, `/trades`, `/export`. `GET /healthz` без токена.

---

## Optimizer

Работает по CSV (`data/history/`), не по live WS.

```bash
export BCS_REFRESH_TOKEN=...
make build-optimizer
make sync-history          # полный universe → data/history/*.csv
make sync-history TIMEFRAME=M15   # родные M15 брокера → data/history-m15/*.csv

# Таймфрейм бэктеста — `candle_timeframe` в YAML или флаг -timeframe M15;
# история берётся из папки этого таймфрейма, бары не на его сетке — ошибка.

make optimizer-orc         # → results/orc/
make optimizer-or-fade
make optimizer-afternoon

go run ./cmd/optimizer portfolio-backtest \
  -config configs/runs/paper-m15.yaml
```

| Вопрос | Документ |
|---|---|
| Solo vs portfolio | [`optimizer-modes.md`](optimizer-modes.md) |
| CLI флаги | [`cmd/optimizer/README.md`](../cmd/optimizer/README.md) |
| Состояние слотов | [`analysis/state.md`](analysis/state.md) |
| Baseline | [`baseline.md`](baseline.md) |

---

## Частые ошибки

| Симптом | Что сделать |
|---|---|
| Админка с ноутбука не открывается на VM | `HTTP_LISTEN=0.0.0.0:8091` + `ADMIN_TOKEN` |
| Бот не стартует с `0.0.0.0` | задать `ADMIN_TOKEN` |
| Connection refused на PUBLIC_IP | открыть TCP 8091 |
| `sync-history` падает | задать `BCS_REFRESH_TOKEN` |
| Optimizer «нет истории» | `make sync-history` (для M15 — `TIMEFRAME=M15`) |


## Цикл разбора

### 1. Забрать данные с хоста

Обычный путь — кнопка **«Скачать incident.json»** на `/export`, копировать БД не нужно:

```bash
make analyze-json JSON=~/Downloads/incident.json
```

Отчёт при этом **побитово совпадает** с разбором по БД: имена полей в выгрузке
один-в-один с колонками `closed_trades`, зеркало держит тест
`TestВыгрузкаЗеркалитСхемуТаблицы`. Из JSON доступны ещё и отклонённые сигналы,
которых в `--db`-режиме нет вовсе.

Ограничение: выгрузка сужена фильтром дат на странице, поэтому «новых сделок»
считается относительно файла, а не всей базы — скрипт об этом пишет.

Копия БД нужна, только если хочется разобрать всю историю целиком. Тогда —
**онлайн-бэкапом**, не `scp`:

```bash
ssh -i ~/.ssh/id_rsa user1@ХОСТ "sqlite3 ~/bcs-trading-bot/data/trades.db \".backup '/tmp/t.db'\""
scp -i ~/.ssh/id_rsa user1@ХОСТ:/tmp/t.db data/trades.db
```

### 2. Досинхронизировать историю

```bash
export BCS_REFRESH_TOKEN=... && make sync-history && make sync-history TIMEFRAME=M15
```

M5 — для проверки филов (бар мельче, чем у бота), M15 — для поштучной сверки с backtest
(`portfolio-backtest -config configs/runs/paper-m15.yaml`). Без этого нечем проверять фил: `data/history/*.csv` отстанет от периода сделок, и
`dead_on_arrival`, `same_bar`, `left_on_table_r` посчитаются не полностью или никак.
**Цены в `trades.db` — это то, что записал бот, а не то, что было на рынке.**

### 3. Посмотреть, что накопилось

```bash
make analyze        # «с прошлого разбора: N новых сделок» + проверка отпечатка конфига
make analyze-new    # метрики только по новым сделкам
```

Если `make analyze` ругается **«КОНФИГ ИЗМЕНИЛСЯ»** — дальше не считать, пока не решено,
что делать со старым периодом: сделки до и после правки несравнимы, их надо
заархивировать (`data/archives.json`), иначе `expectancy_r` усреднит две популяции.

Если печатает «сделок не осталось» — сначала проверить архив, а не пустую БД.

### 4. Что смотреть, в каком порядке

**Механика прежде доходности.** `dead_on_arrival_share`, `same_bar_share`,
`median_pos_at_open_r` видны уже на полусотне сделок. `expectancy_r` обсуждать
после ~200, не раньше.

Приёмочные критерии после правок 2026-09-11 — то, что должно было измениться:

| что | ожидание | где смотреть |
|-----|----------|--------------|
| кап по кэшу | **0 сделок** (было 9 из 10) | секция «Сайзинг» |
| цена 1R | **≈ 400 · (1 + slippage / ширина стопа)**, 404…418 ₽ при 1 б.п. — объём от уровня сигнала, R от фила | там же, «разброс x1.0» |
| стопы | **все ≥ 20 б.п.** (были 12.3 и 17.6) | «медиана R», нижний квартиль |
| `dead_on_arrival_share` | стремится к 0 (было 0.10) | «Качество входа» |
| `same_bar_share` | < 0.2 (было 0.20, на границе) | там же |
| лаг фида | новая метрика, эталона нет — набираем | «Лаг свечного фида» |

Разошлось с ожиданием — это диагностический сигнал, а не шум. `docs/baseline.md`
обещает exp_R +0.418; расхождение с live объяснять, а не списывать на режим рынка.

### 5. Записать выводы

Разбор с выводами — отдельным файлом `docs/analysis/NNNN-*.md`, старые не переписывать.
`docs/analysis/state.md` обновить: гипотезы, долги, последний срез.

### 6. Сдвинуть границу

```bash
make analyze-mark LABEL="что именно разобрали"
```

Только этой командой. Знак не двигается сам: обновляйся он на каждом прогоне, первый
же случайный запуск съел бы границу, ради которой всё и заводилось.

`data/analysis/review-state.json` коммитится вместе с `data/archives.json` и
`data/trades.db` — все трое определяют текущую выборку, разъедутся на другой машине
будут другие цифры.

### Если по ходу меняли конфиг

Порядок обратный: сначала заархивировать прошлый период, потом переснять
`docs/baseline.md` (`portfolio-backtest`), потом ставить знак. Параметры слотов
после смены механики фила, стопа или гейта недействительны — нужна переоптимизация.
