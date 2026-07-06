package strategy

import "fmt"

// DefaultSearchSpacePath возвращает путь к search-space по умолчанию.
func DefaultSearchSpacePath(strategyID string) (string, error) {
	d, err := Get(ResolveType(strategyID))
	if err != nil {
		return "", err
	}
	if d.DefaultSearchSpace == "" {
		return "", fmt.Errorf("стратегия %q: search space не задан", strategyID)
	}
	return d.DefaultSearchSpace, nil
}
