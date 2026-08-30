package models

import "time"

// ViewArchive — сохранённая закладка периода в админке.
type ViewArchive struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	DateFrom  string    `json:"date_from"`
	DateTo    string    `json:"date_to"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}
