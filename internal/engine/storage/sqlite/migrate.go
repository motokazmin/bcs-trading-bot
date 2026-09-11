package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
)

func applyMigrations(db *sql.DB) error {
	if _, err := db.Exec(migration001); err != nil {
		return fmt.Errorf("миграция 001: %w", err)
	}
	if err := applyMigration002(db); err != nil {
		return fmt.Errorf("миграция 002: %w", err)
	}
	if err := applyMigration003(db); err != nil {
		return fmt.Errorf("миграция 003: %w", err)
	}
	if err := applyMigration004(db); err != nil {
		return fmt.Errorf("миграция 004: %w", err)
	}
	if err := applyMigration005(db); err != nil {
		return fmt.Errorf("миграция 005: %w", err)
	}
	if err := applyMigration006(db); err != nil {
		return fmt.Errorf("миграция 006: %w", err)
	}
	if err := applyMigration007(db); err != nil {
		return fmt.Errorf("миграция 007: %w", err)
	}
	// Идемпотентно: подхватывает новые строки, если старый бинарник снова записал closed_at в Local.
	if err := normalizeClosedAtSkew(db); err != nil {
		return fmt.Errorf("normalize closed_at: %w", err)
	}
	return nil
}

func applyMigration002(db *sql.DB) error {
	if err := addColumnIfMissing(db, "closed_trades", "experiment_id", `TEXT NOT NULL DEFAULT 'default'`); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "closed_trades", "stop_mode", `TEXT NOT NULL DEFAULT 'range'`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_closed_trades_experiment
		    ON closed_trades (experiment_id, closed_at)`); err != nil {
		return fmt.Errorf("index experiment: %w", err)
	}
	return nil
}

func applyMigration003(db *sql.DB) error {
	return addColumnIfMissing(db, "closed_trades", "mfe_in_r", `REAL NOT NULL DEFAULT 0`)
}

func applyMigration004(db *sql.DB) error {
	if err := addColumnIfMissing(db, "closed_trades", "mae_in_r", `REAL NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "closed_trades", "breakout_upper", `REAL NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	return addColumnIfMissing(db, "closed_trades", "breakout_lower", `REAL NOT NULL DEFAULT 0`)
}

// applyMigration005 чинит closed_at/recorded_at, сохранённые через Format() в Local (MSK),
// тогда как opened_at уже был UTC. Эталон — hold_seconds (считается по абсолютному Instant).
func applyMigration005(db *sql.DB) error {
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='_migration_005_utc_times'`).Scan(&name)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("check migration marker: %w", err)
	}

	if err := normalizeClosedAtSkew(db); err != nil {
		return err
	}

	if _, err := db.Exec(`CREATE TABLE _migration_005_utc_times (applied_at TEXT NOT NULL DEFAULT (datetime('now')))`); err != nil {
		return fmt.Errorf("migration marker: %w", err)
	}
	return nil
}

func applyMigration006(db *sql.DB) error {
	if err := addColumnIfMissing(db, "closed_trades", "audit_severity", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "closed_trades", "audit_codes", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "closed_trades", "entry_bar_time", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	return addColumnIfMissing(db, "closed_trades", "entry_bar_close", `REAL NOT NULL DEFAULT 0`)
}

// applyMigration007 добавляет факты, которые раньше существовали только в логе:
// объём до капа по кэшу, свободный кэш на входе, возраст свечи входа — и таблицу
// отклонённых сигналов. См. docs/analysis/0002-live-config-drift-and-cash-sizing.md
func applyMigration007(db *sql.DB) error {
	if err := addColumnIfMissing(db, "closed_trades", "requested_quantity", `INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "closed_trades", "cash_at_open", `REAL NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "closed_trades", "bar_age_seconds", `REAL NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if _, err := db.Exec(`
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
		)`); err != nil {
		return fmt.Errorf("create rejected_signals: %w", err)
	}
	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_rejected_signals_date
		    ON rejected_signals (trading_date, experiment_id)`); err != nil {
		return fmt.Errorf("index rejected_signals: %w", err)
	}
	return nil
}

// normalizeClosedAtSkew выравнивает closed_at/recorded_at по hold_seconds, если они разъехались
// (типично: opened_at UTC wall, closed_at Local MSK wall → +3ч). Безопасно вызывать при каждом Open.
func normalizeClosedAtSkew(db *sql.DB) error {
	_, err := db.Exec(`
		UPDATE closed_trades
		SET
			recorded_at = datetime(
				recorded_at,
				printf('%+d seconds', hold_seconds - (strftime('%s', closed_at) - strftime('%s', opened_at)))
			),
			closed_at = datetime(opened_at, printf('+%d seconds', hold_seconds))
		WHERE hold_seconds > 0
		  AND ABS((strftime('%s', closed_at) - strftime('%s', opened_at)) - hold_seconds) > 30`)
	if err != nil {
		return fmt.Errorf("normalize closed_at skew: %w", err)
	}
	return nil
}

func addColumnIfMissing(db *sql.DB, table, column, definition string) error {
	exists, err := hasColumn(db, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	if err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, fmt.Errorf("pragma table_info %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			ctype      string
			notnull    int
			dfltValue  sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &primaryKey); err != nil {
			return false, err
		}
		if strings.EqualFold(name, column) {
			return true, nil
		}
	}
	return false, rows.Err()
}
