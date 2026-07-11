// Package costs — модель торговых издержек (комиссия) для backtest и runtime.
package costs

import (
	"fmt"
	"strings"
)

const (
	ClassCodeStocks  = "TQBR"
	ClassCodeFutures = "SPBFUT"

	// DefaultCommissionRatePerLeg — ставка БКС «Трейдер» (0,008% за leg).
	DefaultCommissionRatePerLeg = 0.00008
	// DefaultPerLotStocks — legacy flat round-trip за акцию.
	DefaultPerLotStocks = 0.10
	// DefaultPerLotFutures — round-trip за контракт фьючерса.
	DefaultPerLotFutures = 5.0
)

// Config задаёт издержки в YAML-конфиге бота и optimizer universe.
type Config struct {
	// CommissionPerLot — flat round-trip за единицу quantity (акция или контракт), руб.
	CommissionPerLot float64 `yaml:"commission_per_lot"`
	// CommissionRatePerLeg — доля оборота за одну сделку (0.00008 = 0,008%).
	CommissionRatePerLeg float64 `yaml:"commission_rate_per_leg"`
}

func (c Config) UsesRate(classCode string) bool {
	return c.usesRate(classCode)
}

func (c Config) usesRate(classCode string) bool {
	if c.CommissionRatePerLeg > 0 {
		return true
	}
	if c.CommissionPerLot > 0 {
		return false
	}
	return !isFutures(classCode)
}

func (c Config) ratePerLeg(classCode string) float64 {
	if c.CommissionRatePerLeg > 0 {
		return c.CommissionRatePerLeg
	}
	return DefaultCommissionRatePerLeg
}

func (c Config) flatPerLot(classCode string) float64 {
	if c.CommissionPerLot > 0 {
		return c.CommissionPerLot
	}
	return DefaultPerLot(classCode)
}

// PerLot возвращает flat round-trip за единицу quantity (legacy API).
func (c Config) PerLot(classCode string) float64 {
	if c.usesRate(classCode) {
		return 0
	}
	return c.flatPerLot(classCode)
}

// DefaultPerLot — flat комиссия по умолчанию для класса инструмента.
func DefaultPerLot(classCode string) float64 {
	if isFutures(classCode) {
		return DefaultPerLotFutures
	}
	return DefaultPerLotStocks
}

// ResolveCosts применяет CLI-override к конфигу из YAML.
func ResolveCosts(perLotFlag, rateFlag float64, classCode string, cfg Config) Config {
	out := cfg
	if rateFlag > 0 {
		out.CommissionRatePerLeg = rateFlag
		out.CommissionPerLot = 0
		return out
	}
	if perLotFlag > 0 {
		out.CommissionPerLot = perLotFlag
		out.CommissionRatePerLeg = 0
		return out
	}
	return out
}

// ResolveFlag — legacy: явное значение CLI (>0) или flat fallback.
func ResolveFlag(flagValue float64, classCode string, cfg Config) float64 {
	if flagValue > 0 {
		return flagValue
	}
	return cfg.PerLot(classCode)
}

// RoundTrip — суммарная комиссия round-trip за сделку, руб.
func RoundTrip(cfg Config, classCode string, entry, exit float64, quantity int, stepPrice float64) float64 {
	if quantity <= 0 {
		return 0
	}
	if stepPrice <= 0 {
		stepPrice = 1
	}
	qty := float64(quantity)
	if cfg.usesRate(classCode) {
		rate := cfg.ratePerLeg(classCode)
		return rate*entry*qty*stepPrice + rate*exit*qty*stepPrice
	}
	return cfg.flatPerLot(classCode) * qty
}

// NetPnL вычитает комиссию round-trip из gross PnL.
func NetPnL(grossPnL float64, cfg Config, classCode string, entry, exit float64, quantity int, stepPrice float64) float64 {
	return grossPnL - RoundTrip(cfg, classCode, entry, exit, quantity, stepPrice)
}

// BreakevenOffset — смещение SL в цене для покрытия комиссии (на акцию).
func BreakevenOffset(cfg Config, classCode string, entryPrice, stepPrice float64) float64 {
	if stepPrice <= 0 {
		stepPrice = 1
	}
	if cfg.usesRate(classCode) {
		return 2 * cfg.ratePerLeg(classCode) * entryPrice
	}
	return cfg.flatPerLot(classCode) / stepPrice
}

// Description — краткое описание модели для экспорта/логов.
func (c Config) Description(classCode string) string {
	if c.usesRate(classCode) {
		return fmt.Sprintf("%.4g%% за leg (%% оборота)", c.ratePerLeg(classCode)*100)
	}
	return fmt.Sprintf("%.2f ₽ round-trip за единицу quantity", c.flatPerLot(classCode))
}

func isFutures(classCode string) bool {
	return strings.EqualFold(strings.TrimSpace(classCode), ClassCodeFutures)
}
