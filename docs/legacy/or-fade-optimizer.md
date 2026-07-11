# OR Fade — optimizer (legacy)

> **Архив.** Champion FROZEN: [`../champion-or-fade.md`](../champion-or-fade.md), [`../strategy-research.md`](../strategy-research.md).

**Статус: FROZEN** — не запускать optimizer без явного запроса.

## Champion (wave1-conservative, 2026-07-11)

| | |
|---|---|
| Expectancy | **+0.25R** |
| PF | 1.49 |
| PnL | +33k / ~2 года |
| Сделок | 133 |
| WF | 15/23 |
| Тикеры | **LKOH, CHMF, MOEX** |

Best config: `configs/champions/or-fade-wave1-conservative.yaml`

## Принцип

1. Строим opening range (первые N минут)
2. Фиксируем пробой за порог
3. Если цена **возвращается** в диапазон → вход **против** пробоя (fade)
4. Слот: ~10:15–12:30 (`fadeTradeEndMinutes`)

## Реестр

`results/or-fade/runs-registry.json`

## Запуск optimizer (только по явному запросу)

```bash
# FROZEN — не запускать без причины
ORF_RUN_ID=research ORF_TICKERS=configs/shared/tickers-or-fade-conservative.yaml make optimizer-or-fade
```

## Архив: старый universe MGNT/ROSN/TATN

Wave1 (2026-07-10): +0.28R seed2, 106 сделок — заменён conservative universe.
