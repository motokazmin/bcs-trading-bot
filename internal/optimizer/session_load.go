package optimizer

import (
	"fmt"
	"os"
	"strings"

	"bcs-trading-bot/internal/config"

	"gopkg.in/yaml.v3"
)

// LoadSessionFromStrategyFile читает секцию session из strategy/search-space YAML.
// Если секции нет — возвращает DefaultSession().
func LoadSessionFromStrategyFile(path string) (config.SessionConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config.SessionConfig{}, fmt.Errorf("чтение session из %q: %w", path, err)
	}
	var wrapped struct {
		Session config.SessionConfig `yaml:"session"`
	}
	if err := yaml.Unmarshal(data, &wrapped); err != nil {
		return config.SessionConfig{}, fmt.Errorf("разбор session: %w", err)
	}
	s := wrapped.Session
	def := DefaultSession()
	if strings.TrimSpace(s.Timezone) == "" {
		s.Timezone = def.Timezone
	}
	if strings.TrimSpace(s.SessionOpenTime) == "" {
		s.SessionOpenTime = def.SessionOpenTime
	}
	if strings.TrimSpace(s.EODCloseTime) == "" {
		s.EODCloseTime = def.EODCloseTime
	}
	return s, nil
}

// DefaultSession возвращает session config по умолчанию.
func DefaultSession() config.SessionConfig {
	return config.SessionConfig{
		Timezone:          "Europe/Moscow",
		EODCloseTime:      "18:40",
		SessionOpenTime:   "10:00",
		EntryDelayMinutes: 0,
	}
}
