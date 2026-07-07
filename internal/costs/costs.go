// Package costs — модель торговых издержек (комиссия) для backtest и runtime.
package costs

import "strings"

const (
	ClassCodeStocks  = "TQBR"
	ClassCodeFutures = "SPBFUT"

	// DefaultPerLotStocks — round-trip за акцию (≈0.03–0.05% оборота при типичном тарифе).
	DefaultPerLotStocks = 0.10
	// DefaultPerLotFutures — round-trip за контракт фьючерса.
	DefaultPerLotFutures = 5.0
)

// Config задаёт издержки в YAML-конфиге бота и optimizer universe.
type Config struct {
	// CommissionPerLot — комиссия round-trip за единицу quantity (акция или контракт), руб.
	CommissionPerLot float64 `yaml:"commission_per_lot"`
}

// PerLot возвращает комиссию за единицу quantity: из конфига или default по class_code.
func (c Config) PerLot(classCode string) float64 {
	if c.CommissionPerLot > 0 {
		return c.CommissionPerLot
	}
	return DefaultPerLot(classCode)
}

// DefaultPerLot — комиссия по умолчанию для класса инструмента.
func DefaultPerLot(classCode string) float64 {
	switch strings.ToUpper(strings.TrimSpace(classCode)) {
	case ClassCodeFutures:
		return DefaultPerLotFutures
	default:
		return DefaultPerLotStocks
	}
}

// ResolveFlag возвращает явное значение CLI-флага (>0) или fallback из конфига/class_code.
func ResolveFlag(flagValue float64, classCode string, cfg Config) float64 {
	if flagValue > 0 {
		return flagValue
	}
	return cfg.PerLot(classCode)
}

// NetPnL вычитает комиссию round-trip из gross PnL.
func NetPnL(grossPnL float64, quantity int, commissionPerLot float64) float64 {
	return grossPnL - commissionPerLot*float64(quantity)
}

// RoundTripTotal — суммарная комиссия за сделку.
func RoundTripTotal(quantity int, commissionPerLot float64) float64 {
	return commissionPerLot * float64(quantity)
}
