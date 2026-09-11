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
	// Of — имя другого параметра, ДОЛЕЙ которого задаётся этот. Тогда Min/Max —
	// границы доли, а значение = доля × значение Of.
	//
	// Нужно для зависимостей, которые нельзя выразить независимыми диапазонами.
	// Пример: trailActivationR должен быть НИЖЕ rewardRatio, иначе позиция закроется
	// по тейку раньше, чем включится трейл, — параметр мёртв, градиента по нему нет,
	// и оптимизатор берёт его случайным. Так уже вышло дважды: у живого чемпиона
	// or-fade активация 2.0039 при тейке 1.2710, и переподбор выбрал такое же снова
	// (docs/analysis/0003-reoptimization-protocol.md).
	Of string `yaml:"of"`
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

	// Сначала независимые параметры: относительные ссылаются на их значения.
	for name, bounds := range s.Parameters {
		if bounds.Of != "" {
			continue
		}
		out[name] = sampleBounds(rng, bounds)
	}
	// Fixed до относительных: база может быть константой, а не поиском.
	s.ApplyFixed(out)

	for name, bounds := range s.Parameters {
		if bounds.Of == "" {
			continue
		}
		if _, fixed := s.Fixed[name]; fixed {
			continue // явная константа сильнее вычисленной доли
		}
		base, ok := out[bounds.Of]
		if !ok {
			// Ссылка в никуда: молча дать ноль — значит тихо убить параметр,
			// поэтому берём долю как абсолютное значение и это видно в отчёте.
			out[name] = sampleBounds(rng, bounds)
			continue
		}
		out[name] = sampleBounds(rng, bounds) * base
	}
	return out
}

func sampleBounds(rng *rand.Rand, b ParamBounds) float64 {
	if b.Type == ParamInt {
		lo, hi := int(b.Min), int(b.Max)
		if hi < lo {
			lo, hi = hi, lo
		}
		return float64(lo + rng.Intn(hi-lo+1))
	}
	return b.Min + rng.Float64()*(b.Max-b.Min)
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
