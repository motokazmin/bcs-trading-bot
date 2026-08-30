package interfaces

import "bcs-trading-bot/internal/models"

// PositionSource отдаёт снимок открытой позиции (или nil).
type PositionSource interface {
	Label() string
	Ticker() string
	ExperimentID() string
	SnapshotPosition() *models.PositionSnapshot
}
