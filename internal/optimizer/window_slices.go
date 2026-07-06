package optimizer

import (
	"bcs-trading-bot/pkg/models"
)

// WindowCandleSlices — преднарезанные свечи по тикерам для train/test одного окна.
type WindowCandleSlices struct {
	Train map[string][]models.Candle
	Test  map[string][]models.Candle
}

// BuildWindowCandleSlices нарезает историю по walk-forward окнам один раз при старте.
func BuildWindowCandleSlices(windows []Window, tickers []string, candleData map[string][]models.Candle) []WindowCandleSlices {
	out := make([]WindowCandleSlices, len(windows))
	for i, w := range windows {
		train := make(map[string][]models.Candle, len(tickers))
		test := make(map[string][]models.Candle, len(tickers))
		for _, ticker := range tickers {
			candles, ok := candleData[ticker]
			if !ok {
				continue
			}
			train[ticker] = FilterCandles(candles, w.TrainStart, w.TrainEnd)
			test[ticker] = FilterCandles(candles, w.TestStart, w.TestEnd)
		}
		out[i] = WindowCandleSlices{Train: train, Test: test}
	}
	return out
}
