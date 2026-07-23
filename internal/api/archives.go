package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bcs-trading-bot/pkg/models"

	"github.com/google/uuid"
)

var (
	ErrArchiveNotFound  = errors.New("архив не найден")
	ErrArchiveDuplicate = errors.New("архив с таким периодом уже существует")
	ErrInvalidDateRange = errors.New("некорректный период дат")
)

// ArchiveStore хранит закладки периодов в JSON-файле.
type ArchiveStore struct {
	path string
}

func NewArchiveStore(path string) *ArchiveStore {
	return &ArchiveStore{path: path}
}

func archiveName(dateFrom, dateTo string) string {
	return dateFrom + " — " + dateTo
}

func parseISODate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("пустая дата")
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("ожидается YYYY-MM-DD: %w", err)
	}
	return t, nil
}

func validateDateRange(dateFrom, dateTo string) error {
	from, err := parseISODate(dateFrom)
	if err != nil {
		return fmt.Errorf("date_from: %w", err)
	}
	to, err := parseISODate(dateTo)
	if err != nil {
		return fmt.Errorf("date_to: %w", err)
	}
	if from.After(to) {
		return ErrInvalidDateRange
	}
	return nil
}

func (s *ArchiveStore) load() ([]models.ViewArchive, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var archives []models.ViewArchive
	if err := json.Unmarshal(data, &archives); err != nil {
		return nil, err
	}
	return archives, nil
}

func (s *ArchiveStore) save(archives []models.ViewArchive) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(archives, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(s.path, data, 0o644)
}

func (s *ArchiveStore) List() ([]models.ViewArchive, error) {
	archives, err := s.load()
	if err != nil {
		return nil, err
	}
	sort.Slice(archives, func(i, j int) bool {
		return archives[i].CreatedAt.After(archives[j].CreatedAt)
	})
	return archives, nil
}

func (s *ArchiveStore) Create(dateFrom, dateTo, comment string) (models.ViewArchive, error) {
	dateFrom = strings.TrimSpace(dateFrom)
	dateTo = strings.TrimSpace(dateTo)
	comment = strings.TrimSpace(comment)

	if err := validateDateRange(dateFrom, dateTo); err != nil {
		return models.ViewArchive{}, err
	}

	archives, err := s.load()
	if err != nil {
		return models.ViewArchive{}, err
	}
	for _, a := range archives {
		if a.DateFrom == dateFrom && a.DateTo == dateTo {
			return models.ViewArchive{}, ErrArchiveDuplicate
		}
	}

	archive := models.ViewArchive{
		ID:        uuid.NewString(),
		Name:      archiveName(dateFrom, dateTo),
		DateFrom:  dateFrom,
		DateTo:    dateTo,
		Comment:   comment,
		CreatedAt: time.Now().UTC(),
	}
	archives = append(archives, archive)
	if err := s.save(archives); err != nil {
		return models.ViewArchive{}, err
	}
	return archive, nil
}

func (s *ArchiveStore) Delete(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrArchiveNotFound
	}
	archives, err := s.load()
	if err != nil {
		return err
	}
	found := false
	filtered := archives[:0]
	for _, a := range archives {
		if a.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, a)
	}
	if !found {
		return ErrArchiveNotFound
	}
	return s.save(filtered)
}
