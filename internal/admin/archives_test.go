package admin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func tempArchiveStore(t *testing.T) *ArchiveStore {
	t.Helper()
	dir := t.TempDir()
	return NewArchiveStore(filepath.Join(dir, "archives.json"))
}

func TestArchiveStoreCreateListDelete(t *testing.T) {
	store := tempArchiveStore(t)

	created, err := store.Create("2026-06-01", "2026-06-29", "тестовый период")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "2026-06-01 — 2026-06-29" {
		t.Fatalf("Name: got %q, want %q", created.Name, "2026-06-01 — 2026-06-29")
	}
	if created.Comment != "тестовый период" {
		t.Fatalf("Comment: got %q", created.Comment)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len: got %d, want 1", len(list))
	}

	if err := store.Delete(created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, err = store.List()
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List after delete: got %d, want 0", len(list))
	}
}

func TestArchiveStoreDuplicate(t *testing.T) {
	store := tempArchiveStore(t)
	if _, err := store.Create("2026-06-01", "2026-06-29", "first"); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	_, err := store.Create("2026-06-01", "2026-06-29", "second")
	if !errors.Is(err, ErrArchiveDuplicate) {
		t.Fatalf("expected ErrArchiveDuplicate, got %v", err)
	}
}

func TestArchiveStoreValidation(t *testing.T) {
	store := tempArchiveStore(t)

	tests := []struct {
		name     string
		from, to string
		wantErr  error
	}{
		{"empty from", "", "2026-06-29", nil},
		{"empty to", "2026-06-01", "", nil},
		{"invalid format", "01.06.2026", "2026-06-29", nil},
		{"from after to", "2026-06-30", "2026-06-01", ErrInvalidDateRange},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.Create(tt.from, tt.to, "")
			if err == nil {
				t.Fatal("expected error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestArchiveStoreDeleteNotFound(t *testing.T) {
	store := tempArchiveStore(t)
	err := store.Delete("missing-id")
	if !errors.Is(err, ErrArchiveNotFound) {
		t.Fatalf("got %v, want ErrArchiveNotFound", err)
	}
}

func TestArchiveStorePersistsToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archives.json")
	store := NewArchiveStore(path)

	created, err := store.Create("2026-01-15", "2026-01-20", "jan")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	reloaded := NewArchiveStore(path)
	list, err := reloaded.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len: got %d, want 1", len(list))
	}
	if list[0].ID != created.ID {
		t.Fatalf("ID mismatch: got %q, want %q", list[0].ID, created.ID)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestArchiveName(t *testing.T) {
	got := archiveName("2026-06-01", "2026-06-29")
	want := "2026-06-01 — 2026-06-29"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
