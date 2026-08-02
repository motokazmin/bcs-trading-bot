# Runbook: бот и optimizer

Краткая шпаргалка «как запустить». Детали стратегий — [`strategy-research.md`](strategy-research.md), режимы optimizer — [`optimizer-modes.md`](optimizer-modes.md), флаги CLI — [`cmd/optimizer/README.md`](../cmd/optimizer/README.md).

---

## Содержание

- [Переменные окружения](#переменные-окружения)
- [Бот: локально](#бот-локально)
- [Бот: облако / VM](#бот-облако--vm)
- [Логи на VM](#логи-на-vm)
- [Почему 127.0.0.1 vs 0.0.0.0](#почему-127001-vs-0000)
- [Админка](#админка)
- [Optimizer](#optimizer)
- [Частые ошибки](#частые-ошибки)

---

## Переменные окружения

| Переменная | Нужна для | Обязательность |
|---|---|---|
| `BCS_REFRESH_TOKEN` | Бот (WS/API), `sync-history` | Да для бота и догрузки истории |
| `ADMIN_TOKEN` | HTTP-админка с публичного IP | Да при bind не на localhost |
| `HTTP_LISTEN` | Адрес `-http-listen` через `make bot` | Нет (дефолт `127.0.0.1:8091`) |
| `BOT_CONFIG` | YAML для `make bot` | Нет (дефолт `configs/runs/portfolio-paper.yaml`) |
| `LOG_FILE` | Путь лог-файла (`-log-file`) | Нет (дефолт `/var/log/trading-bot/bot.log`; `-` — только stdout) |

```bash
export BCS_REFRESH_TOKEN="..."
# облако:
export ADMIN_TOKEN="$(openssl rand -hex 32)"
export HTTP_LISTEN=0.0.0.0:8091
# один раз: каталог под лог (если ещё нет)
# sudo mkdir -p /var/log/trading-bot && sudo chown "$USER" /var/log/trading-bot
```

Токены только в env, не в YAML.

---

## Бот: локально

На своей машине админка нужна только с этого же хоста → слушаем **localhost**.

```bash
make build
export BCS_REFRESH_TOKEN=...

make bot
# то же самое:
# bin/bot -config configs/runs/portfolio-paper.yaml -http-listen 127.0.0.1:8091

# браузер на этой же машине:
open http://127.0.0.1:8091   # или вставить URL вручную
```

`ADMIN_TOKEN` локально **не обязателен** (bind на loopback).

Другие цели:

| Команда | Что делает |
|---|---|
| `make bot` | Paper portfolio, 5 champions |
| `make bot-futures` | Paper фьючерсы (legacy) |
| `make bot-real` | Реал (осторожно) |
| `make bot-smoke` | Smoke OAuth+WS |

Остановка: `Ctrl+C`.

---

## Бот: облако / VM

С ноутбука открываете админку по **публичному IP** VM → бот должен слушать **все интерфейсы**, не только loopback.

```bash
make build
export BCS_REFRESH_TOKEN=...
export ADMIN_TOKEN="$(openssl rand -hex 32)"   # сохранить — понадобится в UI
export HTTP_LISTEN=0.0.0.0:8091

make bot
# эквивалент:
# bin/bot -config configs/runs/portfolio-paper.yaml -http-listen 0.0.0.0:8091

# с вашего ПК:
# http://PUBLIC_IP:8091  → ввести ADMIN_TOKEN
# на VM:  tail -f /var/log/trading-bot/bot.log
```

Без `ADMIN_TOKEN` процесс **не стартует** с `0.0.0.0` / `:8091` (защита в коде).

Firewall/security group: открыть TCP **8091** (или другой порт из `HTTP_LISTEN`) для вашего IP.

### Логи на VM

По умолчанию бот пишет в **stdout + `/var/log/trading-bot/bot.log`** (append, без ANSI). Каталог создаётся сам, если есть права записи.

```bash
# один раз:
sudo mkdir -p /var/log/trading-bot
sudo chown "$USER" /var/log/trading-bot

make bot
tail -f /var/log/trading-bot/bot.log

# только stdout:
# LOG_FILE=- make bot
# bin/bot ... -log-file=
```

---

## Почему 127.0.0.1 vs 0.0.0.0

Это адрес **bind** HTTP-сервера (`-http-listen` / `HTTP_LISTEN`), не «URL сайта».

| Bind | Кто может подключиться | Типичный сценарий |
|---|---|---|
| `127.0.0.1:8091` | Только процессы **на той же машине** | Ноутбук / SSH только для логов |
| `0.0.0.0:8091` | Клиенты с **любого** сетевого интерфейса (LAN, интернет, если порт открыт) | Облачная VM + браузер с дома |

Почему после `bin/bot … -http-listen 127.0.0.1:8091` на VM админка «не открывается» с ноутбука: сервер слушает только loopback VM. Запрос на `http://PUBLIC_IP:8091` до процесса **не доходит**. С `0.0.0.0:8091` — доходит (при открытом firewall и `ADMIN_TOKEN`).

`make bot` подставляет `HTTP_LISTEN` (дефолт `127.0.0.1:8091`). В облаке всегда задавайте `HTTP_LISTEN=0.0.0.0:8091`.

---

## Админка

Встроена в тот же процесс, что и торговля (`cmd/bot`).

| | |
|---|---|
| URL локально | `http://127.0.0.1:8091` |
| URL в облаке | `http://PUBLIC_IP:8091` |
| Авторизация | при публичном bind — Bearer / форма с `ADMIN_TOKEN` |
| Без HTTP | `-http-listen ""` |

Страницы: `/` дашборд, `/open` позиции, `/trades` сделки, `/export` выгрузка для ИИ.  
`GET /healthz` — без токена.

Подробнее про экспорт: [README § Веб-админка](../README.md#веб-админка-и-экспорт-для-ии).

---

## Optimizer

Отдельный бинарник. Не путать с ботом: работает по **CSV** (`data/history/`), не по live WS.

### Раз в начале / периодически — история

```bash
export BCS_REFRESH_TOKEN=...
make build-optimizer
make sync-history          # 9 акций TQBR → data/history/*.csv
```

### Solo walk-forward (поиск params)

FROZEN champions **не крутить** без явного запроса. Типичные цели:

```bash
make optimizer-orc         # → results/orc/
make optimizer-or-fade     # → results/or-fade/
make optimizer-afternoon   # → results/afternoon/
make optimizer-run         # sync-history + ORC (удобный дефолт)
make charts-all            # HTML-графики по OPTIMIZER_OUT
```

Параллелизм: `OPTIMIZER_PARALLEL=4 make optimizer-orc`, двухфаза: `OPTIMIZER_TWO_PHASE=1`.

### Portfolio backtest (проверка paper YAML)

```bash
go run ./cmd/optimizer portfolio-backtest \
  -config configs/runs/portfolio-paper.yaml
# или после make build-optimizer:
# bin/optimizer portfolio-backtest -config configs/runs/portfolio-paper.yaml
```

| Вопрос | Команда / док |
|---|---|
| Solo vs portfolio — в чём разница? | [`optimizer-modes.md`](optimizer-modes.md) |
| Все флаги CLI | [`cmd/optimizer/README.md`](../cmd/optimizer/README.md) |
| Методология / champions | [`strategy-research.md`](strategy-research.md) |
| Baseline § C | [`champion-baseline.md`](champion-baseline.md) |

---

## Частые ошибки

| Симптом | Причина | Что сделать |
|---|---|---|
| С ноутбука не открывается админка на VM, `127.0.0.1` в команде | Bind только loopback | `HTTP_LISTEN=0.0.0.0:8091` + `ADMIN_TOKEN` |
| Бот не стартует с `0.0.0.0` | Нет токена | `export ADMIN_TOKEN=...` |
| Connection refused / timeout на PUBLIC_IP | Firewall / неверный порт | Открыть TCP 8091, проверить `HTTP_LISTEN` |
| `make sync-history` падает | Нет `BCS_REFRESH_TOKEN` | Задать env |
| Optimizer «нет истории» | Пустой `data/history` | `make sync-history` |
