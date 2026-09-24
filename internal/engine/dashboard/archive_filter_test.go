package dashboard

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"bcs-trading-bot/internal/engine/api"
	"bcs-trading-bot/internal/models"
)

func serverWithArchive(t *testing.T, from, to string) (*Server, models.ViewArchive) {
	t.Helper()
	store := api.NewArchiveStore(filepath.Join(t.TempDir(), "archives.json"))
	archive, err := store.Create(from, to, "старые сделки")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return &Server{archives: store}, archive
}

func filterFor(s *Server, query string) models.TradeFilter {
	return s.parseFilter(httptest.NewRequest("GET", "/api/trades"+query, nil))
}

// Архив скрыт из общей выборки и виден только при явном выборе.
func TestParseFilterHidesArchives(t *testing.T) {
	s, archive := serverWithArchive(t, "2026-08-10", "2026-08-28")

	tests := []struct {
		name  string
		query string
		hide  bool
	}{
		{"без фильтров", "", true},
		{"период «Все»", "?period=all", true},
		{"период «Сегодня»", "?period=today&date_from=2026-08-30&date_to=2026-08-30", true},
		{"даты вне архива", "?date_from=2026-08-29&date_to=2026-08-30", true},
		{"даты частично перекрывают архив", "?date_from=2026-08-20&date_to=2026-09-05", true},
		{"архив выбран явно", "?period=" + archive.ID + "&date_from=2026-08-10&date_to=2026-08-28", false},
		{"архив выбран, даты потерялись", "?period=" + archive.ID, false},
		{"даты целиком внутри архива", "?date_from=2026-08-12&date_to=2026-08-13", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filterFor(s, tt.query)
			if tt.hide {
				if len(f.ExcludeRanges) != 1 {
					t.Fatalf("ExcludeRanges: got %v, want 1 диапазон", f.ExcludeRanges)
				}
				want := models.DateRange{From: "2026-08-10", To: "2026-08-28"}
				if f.ExcludeRanges[0] != want {
					t.Fatalf("ExcludeRanges[0]: got %+v, want %+v", f.ExcludeRanges[0], want)
				}
				return
			}
			if len(f.ExcludeRanges) != 0 {
				t.Fatalf("ExcludeRanges: got %v, want пусто", f.ExcludeRanges)
			}
		})
	}
}

// Явно выбранный архив не скрывает соседние: прячется всё, кроме выбранного.
func TestParseFilterHidesOtherArchives(t *testing.T) {
	store := api.NewArchiveStore(filepath.Join(t.TempDir(), "archives.json"))
	first, err := store.Create("2026-07-01", "2026-07-31", "июль")
	if err != nil {
		t.Fatalf("Create июль: %v", err)
	}
	if _, err := store.Create("2026-08-01", "2026-08-28", "август"); err != nil {
		t.Fatalf("Create август: %v", err)
	}
	s := &Server{archives: store}

	f := filterFor(s, "?period="+first.ID+"&date_from=2026-07-01&date_to=2026-07-31")
	if len(f.ExcludeRanges) != 1 {
		t.Fatalf("ExcludeRanges: got %v, want 1 диапазон (август)", f.ExcludeRanges)
	}
	if f.ExcludeRanges[0].From != "2026-08-01" {
		t.Fatalf("скрыт не тот архив: %+v", f.ExcludeRanges[0])
	}
}

// Без ArchiveStore фильтр остаётся прежним — админка работает и с отключёнными архивами.
func TestParseFilterWithoutArchiveStore(t *testing.T) {
	s := &Server{}
	if f := filterFor(s, "?date_from=2026-08-01"); len(f.ExcludeRanges) != 0 {
		t.Fatalf("ExcludeRanges: got %v, want пусто", f.ExcludeRanges)
	}
}
