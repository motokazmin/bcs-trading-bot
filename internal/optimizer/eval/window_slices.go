package eval

import (
	core "bcs-trading-bot/internal/optimizer/core"
	"bcs-trading-bot/internal/optimizer/data"
	"bcs-trading-bot/pkg/models"
)

// WindowCandleSlices — преднарезанные свечи по тикерам для одного окна.
type WindowCandleSlices struct {
	Candles map[string][]models.Candle
}

// BuildWindowCandleSlices нарезает историю по walk-forward окнам один раз при старте.
func BuildWindowCandleSlices(windows []core.Window, tickers []string, candleData map[string][]models.Candle) []WindowCandleSlices {
	out := make([]WindowCandleSlices, len(windows))
	for i, w := range windows {
		candles := make(map[string][]models.Candle, len(tickers))
		for _, ticker := range tickers {
			all, ok := candleData[ticker]
			if !ok {
				continue
			}
			candles[ticker] = data.FilterCandles(all, w.Start, w.End)
		}
		out[i] = WindowCandleSlices{Candles: candles}
	}
	return out
}
