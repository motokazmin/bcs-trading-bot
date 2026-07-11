# Legacy-конфиги бота (до рефакторинга 2026-07)

Справочные YAML для **восстановления параметров** старых virtual-экспериментов (`atr-1-lean` и др.).
Бот сейчас не в фокусе; файлы нужны как документация и для ручного сравнения с optimizer.

| Файл | Описание | Период live (из export) |
|---|---|---|
| `bot-experiments-atr-ab.yaml` | 6 A/B: atr-1-lean, atr-2-lean, delayed, +volume | 2026-06-30 … 2026-07-08 |
| `bot-experiments-stop-mode-ab.yaml` | baseline (range) / atr-2 / atr-3 | 2026-06-26 … 2026-06-27 |

## atr-1-lean (лучший momentum на live)

- `type`: momentum_breakout (неявно, без поля `type`)
- `tickers`: SBER, ROSN, NVTK
- `atr_multiplier`: **1.0**
- `lookback`: 20, `stop_mode`: atr
- `reward_ratio`: 3.0 (дефолт), трейлинг — дефолт (+1R / +2R / MFE−1R)
- Live: +0.092R, 51 сделка, +1560 ₽ (2026-06-30 … 07-08)

## Запуск (если понадобится)

```bash
go run ./cmd/bot -config configs/runs/legacy/bot-experiments-atr-ab.yaml
```

## Эквивалент для optimizer

См. `configs/strategies/momentum-breakout-lean.yaml` и [`docs/legacy/momentum-optimizer.md`](../../../docs/legacy/momentum-optimizer.md).
