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
make bot
# → http://127.0.0.1:8091
```

| Команда | Что делает |
|---|---|
| `make bot` | Paper portfolio, 5 champions |
| `make bot-real` | Реал (осторожно; сейчас 1 experiment) |
| `make bot-smoke` | Smoke OAuth+WS |
| `make bot-futures` | Paper фьючерсы (отдельный профиль) |

Остановка: `Ctrl+C`.

---

## Бот: облако / VM

```bash
make build
export BCS_REFRESH_TOKEN=...
export ADMIN_TOKEN="$(openssl rand -hex 32)"
export HTTP_LISTEN=0.0.0.0:8091
make bot
# с ПК: http://PUBLIC_IP:8091 → ADMIN_TOKEN
# на VM:  tail -f /var/log/trading-bot/bot.log
```

Без `ADMIN_TOKEN` процесс не стартует с `0.0.0.0`. Firewall: TCP **8091**.

Логи по умолчанию: stdout + `/var/log/trading-bot/bot.log`. Только stdout: `LOG_FILE=- make bot`.

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
