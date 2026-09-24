Ordering fix (live + backtest) — fillPrice считается до CapQuantityByCash, используется единообразно в капе/риске/ордере:
    internal/strategy/selfmanaged/selfmanaged.go
    internal/backtest/portfolio.go
    internal/backtest/runner.go
Буфер cash_utilization_percent (дефолт 95%):
    internal/config/config.go — новое поле RiskConfig
    internal/strategy/selfmanaged/selfmanaged.go — CashUtilizationPct в Config, применяется к балансу перед капом
    internal/app/trader.go — проброс из RiskConfig в selfmanaged.Config
Лог [CASHCAP] для статистики конкуренции за кэш:
    internal/logx/logx.go — новая функция CashCap
    internal/strategy/selfmanaged/selfmanaged.go — вызов при реальном урезании объёма
