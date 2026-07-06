package strategy

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// BuildContext — контекст сборки стратегии (optimizer / backtest).
type BuildContext struct {
	StopMode string
	Session  SessionTimes
}

// Descriptor описывает фабрику одной стратегии.
type Descriptor struct {
	ID                   string
	DefaultSearchSpace   string
	NewFromParams        func(params Params, ctx BuildContext) (CandleStrategy, error)
	ParamsToConfigFields func(params Params, ctx BuildContext) map[string]interface{}
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Descriptor{}
)

// Register регистрирует стратегию (вызывается из init()).
func Register(d Descriptor) {
	if d.ID == "" {
		panic("strategy: empty ID")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[d.ID]; exists {
		panic("strategy: duplicate register " + d.ID)
	}
	registry[d.ID] = d
}

// Get возвращает descriptor по id.
func Get(id string) (Descriptor, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := registry[id]
	if !ok {
		return Descriptor{}, fmt.Errorf("неизвестная стратегия %q", id)
	}
	return d, nil
}

// ListIDs возвращает отсортированные id зарегистрированных стратегий.
func ListIDs() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for id := range registry {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// NewFromParams создаёт стратегию по id и параметрам optimizer.
func NewFromParams(id string, params Params, ctx BuildContext) (CandleStrategy, error) {
	d, err := Get(id)
	if err != nil {
		return nil, err
	}
	if d.NewFromParams == nil {
		return nil, fmt.Errorf("стратегия %q: NewFromParams не задан", id)
	}
	return d.NewFromParams(params, ctx)
}

// ValidateStrategyType проверяет, что type зарегистрирован.
func ValidateStrategyType(typeID string) error {
	_, err := Get(ResolveType(typeID))
	return err
}

// ResolveType нормализует id (пустой → default).
func ResolveType(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return DefaultType()
	}
	return id
}
