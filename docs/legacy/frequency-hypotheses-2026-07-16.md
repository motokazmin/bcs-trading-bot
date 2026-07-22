# Frequency / whitelist research — закрыто 2026-07-16

Ветка: `research/frequency-hypotheses`.  
Цель была поднять частоту сделок портфеля **без** роста risk%.  
**Итог на дату закрытия:** остались три FROZEN champions main-сессии (ORC, OR Fade, MF Afternoon). Новые линии и candidates **не** вливались в portfolio.
*(После 2026-07-17 в paper добавлены Morning/Evening Session ORC → 5 champions; см. [`strategy-research.md`](../strategy-research.md).)*

Актуальный статус: [`../strategy-research.md`](../strategy-research.md).

## Что искали

1. Новые независимые источники сигналов (VWAP, Midday compression, Late session, Prev-day, Afternoon fade, SBER day-trend).
2. Глубина существующих edge: MF whitelist solo, ORC LKOH, OR Fade +TATN / maxEntries.

## Вердикт кратко

| Линия | Вердикт |
|-------|---------|
| OR Fade maxEntries 1/2/3 | сделки не растут |
| VWAP MGNT/ROSN/TATN | отклонено (seed2 / TATN) |
| VWAP MGNT+ROSN | метрики candidate, **не принято** в portfolio (пересечение тикеров) |
| SBER day-trend / Midday / Late | отклонено |
| MF solo SBER/NVTK/ROSN | отклонено (matrix не переносится) |
| ORC LKOH solo | отклонено (seed2 PF &lt; 1.3) |
| OR Fade +TATN fixed | метрики candidate, **не принято** (TATN уже в ORC/MF) |
| Prev-day / Afternoon range fade | отклонено |

Полный самодостаточный бриф с параметрами и цифрами: [`llm-strategy-research-brief.md`](llm-strategy-research-brief.md).

## Артефакты в репозитории

| Путь | Содержание |
|------|------------|
| `configs/legacy/frequency-hypotheses/strategies/` | search space YAML закрытых линий |
| `configs/legacy/frequency-hypotheses/tickers/` | universe YAML для тех прогонов |
| `configs/champions/legacy/` | snapshot candidates (OR Fade+TATN, VWAP MGNT+ROSN) — не FROZEN |
| `scripts/legacy/` | run-скрипты optimizer для закрытых линий |
| `docs/legacy/champion-vwap-pullback.md` | описание VWAP candidate |
| `results/{vwap-pullback,midday-compression,...}/` | локальные прогоны (не в git обычно) |

Код стратегий в `internal/strategy/` оставлен (как mean_reversion / opening_range): DefaultSearchSpace указывает на `configs/legacy/frequency-hypotheses/strategies/`. Не запускать в production / paper portfolio.

## FROZEN portfolio (на дату закрытия frequency-волны)

```
10:00–10:30  ORC          MGNT, ROSN, TATN
10:15–12:30  OR Fade      LKOH, CHMF, MOEX, AFKS
12:30–18:40  MF Afternoon MGNT, TATN
```

Snapshots: `configs/champions/{orc-wave2,or-fade-wave3-afks,mf-afternoon-wave2-narrow}.yaml`.  
Актуальный paper (5 champions, + morning/evening Session ORC): `configs/runs/portfolio-paper.yaml`.
