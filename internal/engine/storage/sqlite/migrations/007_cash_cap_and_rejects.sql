ALTER TABLE closed_trades ADD COLUMN requested_quantity INTEGER NOT NULL DEFAULT 0;
ALTER TABLE closed_trades ADD COLUMN cash_at_open REAL NOT NULL DEFAULT 0;
ALTER TABLE closed_trades ADD COLUMN bar_age_seconds REAL NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS rejected_signals (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    trading_mode  TEXT NOT NULL DEFAULT '',
    run_id        TEXT NOT NULL DEFAULT '',
    experiment_id TEXT NOT NULL DEFAULT 'default',
    ticker        TEXT NOT NULL DEFAULT '',
    direction     TEXT NOT NULL DEFAULT '',
    reason        TEXT NOT NULL DEFAULT '',
    signal_price  REAL NOT NULL DEFAULT 0,
    stop_loss     REAL NOT NULL DEFAULT 0,
    requested_qty INTEGER NOT NULL DEFAULT 0,
    cash_at_check REAL NOT NULL DEFAULT 0,
    trading_date  TEXT NOT NULL DEFAULT '',
    rejected_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_rejected_signals_date
    ON rejected_signals (trading_date, experiment_id);
