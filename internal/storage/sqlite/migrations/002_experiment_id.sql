ALTER TABLE closed_trades ADD COLUMN experiment_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE closed_trades ADD COLUMN stop_mode TEXT NOT NULL DEFAULT 'range';

CREATE INDEX IF NOT EXISTS idx_closed_trades_experiment
    ON closed_trades (experiment_id, closed_at);
