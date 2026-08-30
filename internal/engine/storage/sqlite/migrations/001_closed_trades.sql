CREATE TABLE IF NOT EXISTS closed_trades (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    trading_mode        TEXT    NOT NULL,
    run_id              TEXT    NOT NULL,
    recorded_at         TEXT    NOT NULL,
    ticker              TEXT    NOT NULL,
    class_code          TEXT    NOT NULL,
    step_price_value    REAL    NOT NULL,
    direction           TEXT    NOT NULL,
    quantity            INTEGER NOT NULL,
    entry_price         REAL    NOT NULL,
    exit_price          REAL    NOT NULL,
    initial_stop_loss   REAL    NOT NULL,
    initial_take_profit REAL    NOT NULL,
    final_stop_loss     REAL    NOT NULL,
    r_distance          REAL    NOT NULL,
    gross_pnl           REAL    NOT NULL,
    pnl_r               REAL    NOT NULL,
    close_reason        TEXT    NOT NULL,
    trail_stage         INTEGER NOT NULL,
    is_winner           INTEGER NOT NULL,
    opened_at           TEXT    NOT NULL,
    closed_at           TEXT    NOT NULL,
    hold_seconds        INTEGER NOT NULL,
    trading_date        TEXT    NOT NULL,
    candle_timeframe    TEXT    NOT NULL,
    lookback            INTEGER NOT NULL,
    risk_per_trade_pct  REAL    NOT NULL,
    deposit_per_ticker  REAL    NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_closed_trades_mode_closed
    ON closed_trades (trading_mode, closed_at);

CREATE INDEX IF NOT EXISTS idx_closed_trades_ticker_date
    ON closed_trades (ticker, trading_date);

CREATE INDEX IF NOT EXISTS idx_closed_trades_close_reason
    ON closed_trades (close_reason);
