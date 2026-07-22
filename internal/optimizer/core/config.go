package core

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParamType — тип параметра в search space.
type ParamType string

const (
	ParamInt   ParamType = "int"
	ParamFloat ParamType = "float"
)

// ParamBounds — границы одного параметра.
type ParamBounds struct {
	Type ParamType `yaml:"type"`
	Min  float64   `yaml:"min"`
	Max  float64   `yaml:"max"`
}

// SearchSpace описывает пространство поиска и фиксированные константы.
type SearchSpace struct {
	Strategy   string                 `yaml:"strategy"`
	Parameters map[string]ParamBounds `yaml:"parameters"`
	Fixed      map[string]float64     `yaml:"fixed"`
}

// ParameterSet — конкретный набор гиперпараметров.
type ParameterSet map[string]float64

// LoadSearchSpace читает YAML search space.
func LoadSearchSpace(path string) (*SearchSpace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("чтение search space %q: %w", path, err)
	}

	// Поддерживаем оба формата:
	// 1) legacy файл search-space.yaml;
	// 2) единый strategy-конфиг с вложенной секцией search_space.
	var wrapped struct {
		SearchSpace SearchSpace `yaml:"search_space"`
	}
	if err := yaml.Unmarshal(data, &wrapped); err != nil {
		return nil, fmt.Errorf("разбор search space: %w", err)
	}
	if len(wrapped.SearchSpace.Parameters) > 0 {
		space := wrapped.SearchSpace
		if strings.TrimSpace(space.Strategy) == "" {
			return nil, fmt.Errorf("search space: strategy пуст")
		}
		return &space, nil
	}

	var space SearchSpace
	if err := yaml.Unmarshal(data, &space); err != nil {
		return nil, fmt.Errorf("разбор search space: %w", err)
	}
	if len(space.Parameters) == 0 {
		return nil, fmt.Errorf("search space: parameters пуст")
	}
	if strings.TrimSpace(space.Strategy) == "" {
		return nil, fmt.Errorf("search space: strategy пуст")
	}
	return &space, nil
}

func (s *SearchSpace) FixedValue(key string, fallback float64) float64 {
	if s.Fixed == nil {
		return fallback
	}
	if v, ok := s.Fixed[key]; ok {
		return v
	}
	return fallback
}

// ApplyFixed копирует fixed-константы в params (поверх sample).
// Без этого флаги вроде longOnly из YAML fixed не доходят до стратегии.
func (s *SearchSpace) ApplyFixed(out ParameterSet) {
	if s == nil || out == nil || len(s.Fixed) == 0 {
		return
	}
	for k, v := range s.Fixed {
		out[k] = v
	}
}

// Sample случайную точку из search space + fixed-константы.
func (s *SearchSpace) Sample(rng *rand.Rand) ParameterSet {
	out := make(ParameterSet, len(s.Parameters)+len(s.Fixed))
	for name, bounds := range s.Parameters {
		switch bounds.Type {
		case ParamInt:
			lo := int(bounds.Min)
			hi := int(bounds.Max)
			if hi < lo {
				lo, hi = hi, lo
			}
			out[name] = float64(lo + rng.Intn(hi-lo+1))
		default:
			out[name] = bounds.Min + rng.Float64()*(bounds.Max-bounds.Min)
		}
	}
	s.ApplyFixed(out)
	return out
}

// IntParam возвращает целочисленный параметр.
func (p ParameterSet) IntParam(key string) int {
	return int(math.Round(p[key]))
}

// FloatParam возвращает float-параметр.
func (p ParameterSet) FloatParam(key string) float64 {
	return p[key]
}

// Keys возвращает отсортированные имена параметров.
func (p ParameterSet) Keys() []string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	return keys
}
