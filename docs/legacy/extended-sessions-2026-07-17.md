# Extended sessions research — закрыто 2026-07-17

Влиты в portfolio как FROZEN: **Morning Session ORC** + **Evening Session ORC**.  
Актуальный статус: [`../strategy-research.md`](../strategy-research.md).

## Цель

Поднять частоту сделок на едином счёте 200k за счёт сессий MOEX вне основной 10:00–18:40.

## Вердикты

| Линия | Вердикт |
|-------|---------|
| Morning Session ORC | **FROZEN** → paper |
| Evening Session ORC | **FROZEN** → paper (seed2 WF 9/23 — жёлтый флаг) |
| Evening OR Fade / Gap drive | отклонено |
| Morning OR Fade / Gap drive | отклонено |
| Weekend (ДСВД) ORC / Fade / Gap | отклонено |

## Конфиги отклонённых линий

`configs/legacy/extended-sessions/strategies/`:

- `session-or-fade-{morning,evening,weekend}.yaml`
- `session-gap-drive-{morning,evening,weekend}.yaml`
- `session-orc-weekend.yaml`

Код `session_or_fade` / `session_gap_drive` оставлен; DefaultSearchSpace указывает сюда. Не запускать в paper.

Активные search space champions: `configs/strategies/session-orc-{morning,evening}.yaml`.
