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
