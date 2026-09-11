# Runbook: бот и optimizer

Шпаргалка запуска. Портфель — [`portfolio.md`](portfolio.md), режимы optimizer — [`optimizer-modes.md`](optimizer-modes.md), CLI — [`cmd/optimizer/README.md`](../cmd/optimizer/README.md).

---

## Переменные окружения

| Переменная | Нужна для | Обязательность |
|---|---|---|
| `BCS_REFRESH_TOKEN` | Бот, `sync-history` | Да для бота и истории |
| `ADMIN_TOKEN` | HTTP-админка с публичного IP | Да при bind не на localhost |
| `HTTP_LISTEN` | Адрес `-http-listen` через `make bot` | Нет (дефолт `127.0.0.1:8091`) |
| `BOT_CONFIG` | YAML для `make bot` | Нет (дефолт `configs/runs/portfolio-paper.yaml`) |
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

make optimizer-orc         # → results/orc/
make optimizer-or-fade
make optimizer-afternoon

go run ./cmd/optimizer portfolio-backtest \
  -config configs/runs/portfolio-paper.yaml
```

| Вопрос | Документ |
|---|---|
| Solo vs portfolio | [`optimizer-modes.md`](optimizer-modes.md) |
| CLI флаги | [`cmd/optimizer/README.md`](../cmd/optimizer/README.md) |
| Champions | [`portfolio.md`](portfolio.md) |
| Baseline | [`baseline.md`](baseline.md) |

---

## Частые ошибки

| Симптом | Что сделать |
|---|---|
| Админка с ноутбука не открывается на VM | `HTTP_LISTEN=0.0.0.0:8091` + `ADMIN_TOKEN` |
| Бот не стартует с `0.0.0.0` | задать `ADMIN_TOKEN` |
| Connection refused на PUBLIC_IP | открыть TCP 8091 |
| `sync-history` падает | задать `BCS_REFRESH_TOKEN` |
| Optimizer «нет истории» | `make sync-history` |


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
export BCS_REFRESH_TOKEN=... && make sync-history
```

Без этого нечем проверять фил: `data/history/*.csv` отстанет от периода сделок, и
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
| цена 1R | **ровно 400 ₽ на каждой** сделке | там же, «разброс x1.0» |
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
`docs/baseline.md` (`portfolio-backtest`), потом ставить знак. Параметры чемпионов
после смены механики фила, стопа или гейта недействительны — нужна переоптимизация.
