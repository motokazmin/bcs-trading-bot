# Roadmap

План работ и недавно закрытые пункты.  
Как устроена система: [`docs/system.md`](docs/system.md). Champions и исследования: [`docs/strategy-research.md`](docs/strategy-research.md).

*Обновлено: 2026-07-17*

---

## Недавно сделано

- [x] Модель комиссии BCS «Трейдер» (0,008% / leg) в bot, optimizer, backtest
- [x] Net PnL и комиссия в `VirtualExecutor`, worker, SQLite
- [x] Commission-rerun champions (ORC, OR Fade, MF) — seed 1
- [x] Baseline доходности — [`docs/champion-baseline.md`](docs/champion-baseline.md)
- [x] Walk-forward optimizer, 3 FROZEN champions, `portfolio-paper.yaml`
- [x] Рефакторинг доков: `docs/system.md` + lean roadmap
- [x] **Единый virtual-счёт** — `portfolio.shared_account`, `CanOpenTicker`, `optimizer portfolio-backtest` (GO 2026-07-17)

---

## Перед live (приоритет)

- [ ] **Paper-валидация portfolio** — 2–4 нед на shared `portfolio-paper.yaml`, сверка с baseline § B
- [ ] **Portfolio на одном real-счёте** — в `real` сейчас только **1 experiment** (`cmd/bot/main.go`):
  - один `BCSClient` на все стратегии (тот же shared-паттерн, что paper);
  - конфиг `portfolio-real.yaml`
- [ ] **ГО / маржа** перед шортами — урезание лота по свободному кэшу
- [ ] Депозит из `GetBalance()` вместо статического `risk.deposit`

---

## Модель исполнения (позже)

- [ ] **Проскальзывание** — backtest: close + intrabar OHLC, без slippage; live: limit in / market out; позже модель в backtest + сравнение fill vs расчёт
- [ ] Реальная комиссия из API брокера
- [ ] Парсинг `broker_order_id` из ответа BCS

---

## Аналитика

- [ ] CLI или дашборд: win rate, avg R, дневной P&L, virtual vs real
- [ ] Опционально PostgreSQL для удалённой аналитики

---

## Инфраструктура

- [ ] Автообновление OAuth refresh token
- [ ] Переподписка WebSocket после реконнекта с новым токеном
- [ ] Unit/integration-тесты: сигнал → открытие → SL/TP/EOD → БД

---

## Research (опционально)

- [ ] Optimizer ранжирует по `expectancy_r`, а не Calmar score — см. [`docs/strategy-research.md`](docs/strategy-research.md)
