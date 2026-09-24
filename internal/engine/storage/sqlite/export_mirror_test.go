package sqlite_test

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"bcs-trading-bot/internal/engine/dashboard"
	"bcs-trading-bot/internal/engine/storage/sqlite"
)

// dashboard.ExportTrade обязан быть точным зеркалом closed_trades: scripts/
// analyze-trades.py читает и БД, и incident.json одним кодом. Разъедутся —
// --from-json начнёт молча терять колонки, а это ровно тот класс дефекта,
// на который ушёл день (см. docs/analysis/0002-*).
func TestВыгрузкаЗеркалитСхемуТаблицы(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "trades.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	dbCols, err := store.ColumnNames("closed_trades")
	if err != nil {
		t.Fatalf("ColumnNames: %v", err)
	}

	blob, err := json.Marshal(dashboard.ExportTrade{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(blob, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var jsonKeys []string
	for k := range m {
		jsonKeys = append(jsonKeys, k)
	}
	sort.Strings(jsonKeys)
	sort.Strings(dbCols)

	missing := diff(dbCols, jsonKeys)
	extra := diff(jsonKeys, dbCols)
	if len(missing) > 0 {
		t.Errorf("колонки БД без поля в ExportTrade: %v — выгрузка их потеряет", missing)
	}
	if len(extra) > 0 {
		t.Errorf("поля ExportTrade без колонки в БД: %v — analyze не найдёт их при чтении из БД", extra)
	}
}

func diff(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[s] = true
	}
	var out []string
	for _, s := range a {
		if !set[s] {
			out = append(out, s)
		}
	}
	return out
}
