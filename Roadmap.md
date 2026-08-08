# Roadmap

Как устроена система: [`docs/system.md`](docs/system.md). Портфель: [`docs/portfolio.md`](docs/portfolio.md).

---

## Перед live

- [ ] Paper-валидация portfolio (2–4 нед), сверка с [`docs/baseline.md`](docs/baseline.md)
- [ ] Portfolio на одном **real**-счёте (сейчас в `real` — один experiment)
- [ ] ГО / маржа перед шортами
- [ ] Депозит из `GetBalance()` вместо статического `risk.deposit`

---

## Модель исполнения

- [ ] Явный slippage в backtest + сравнение fill vs расчёт
- [ ] Комиссия из API брокера
- [ ] Парсинг `broker_order_id` из ответа BCS

---

## Аналитика и инфраструктура

- [ ] Дашборд/CLI: win rate, avg R, дневной P&L
- [ ] Автообновление OAuth refresh token
- [ ] Переподписка WebSocket после реконнекта с новым токеном
- [ ] Больше тестов: сигнал → открытие → SL/TP/EOD → БД

---

## Research (опционально)

- [ ] Ранжирование optimizer по `expectancy_r`, а не только Calmar — см. [`docs/portfolio.md`](docs/portfolio.md)
