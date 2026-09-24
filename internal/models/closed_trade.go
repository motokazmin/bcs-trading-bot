package models

import "time"

// ClosedTrade — снимок закрытой позиции для анализа эффективности.
type ClosedTrade struct {
	// ID — rowid в closed_trades. Заполняется только при чтении; нужен как
	// граница «новых сделок» в data/analysis/review-state.json.
	ID                int64
	RecordedAt        string
	TradingMode       string
	RunID             string
	ExperimentID      string
	StopMode          string
	Ticker            string
	ClassCode         string
	StepPriceValue    float64
	Direction         string
	Quantity          int
	EntryPrice        float64
	ExitPrice         float64
	InitialStopLoss   float64
	InitialTakeProfit float64
	FinalStopLoss     float64
	RDistance         float64
	GrossPnL          float64
	PnLR              float64
	MFEinR            float64 `json:"mfe_in_r"`
	MAEinR            float64 `json:"mae_in_r"`
	BreakoutUpper     float64 `json:"breakout_upper"`
	BreakoutLower     float64 `json:"breakout_lower"`
	CloseReason       string
	TrailStage        int
	IsWinner          bool
	OpenedAt          time.Time
	ClosedAt          time.Time
	HoldSeconds       int
	TradingDate       string
	CandleTimeframe   string
	Lookback           int
	RiskPerTradePct    float64
	DepositPerTicker   float64
	StrategyParamsJSON string `json:"strategy_params,omitempty"`
	AuditSeverity      string `json:"audit_severity,omitempty"`
	AuditCodes         string `json:"audit_codes,omitempty"`
	EntryBarTime       string `json:"entry_bar_time,omitempty"`
	EntryBarClose      float64 `json:"entry_bar_close,omitempty"`
	// RequestedQuantity — объём, который запросил риск-менеджер ДО капа по кэшу.
	// Вместе с Quantity показывает, срезал ли кап позицию: без этой пары факт капа
	// выводится только косвенно, через разброс GrossPnL/PnLR между сделками.
	RequestedQuantity int `json:"requested_quantity,omitempty"`
	// CashAtOpen — свободный кэш на момент расчёта объёма (до резервирования доли).
	CashAtOpen float64 `json:"cash_at_open,omitempty"`
	// BarAgeSeconds — сколько прошло от КОНЦА свечи входа до открытия: лаг свечного
	// фида. До 2026-09-23 считалось от начала свечи по формирующемуся бару (0004).
	BarAgeSeconds float64 `json:"bar_age_seconds,omitempty"`
}

// CashCapped — кап по кэшу реально срезал объём позиции.
func (t ClosedTrade) CashCapped() bool {
	return t.RequestedQuantity > 0 && t.Quantity < t.RequestedQuantity
}

// RejectedSignal — сигнал, до сделки не дошедший. Пишется там, где раньше был
// только logx.SignalRejected: без этого «почему сделок мало» не отвечается по БД.
//
// ВНИМАНИЕ: сюда НЕ попадают отказы гейта min_stop_bps — он живёт внутри слоя
// стратегии (calcStopTP возвращает нулевые SL/TP), и сигнал не рождается вовсе.
// Чтобы считать и их, нужно протащить причину через все вызовы calcStopTP (17 мест).
type RejectedSignal struct {
	ID           int64     `json:"id,omitempty"`
	TradingMode  string    `json:"trading_mode"`
	RunID        string    `json:"run_id"`
	ExperimentID string    `json:"experiment_id"`
	Ticker       string    `json:"ticker"`
	Direction    string    `json:"direction"`
	Reason       string    `json:"reason"`
	SignalPrice  float64   `json:"signal_price,omitempty"`
	StopLoss     float64   `json:"stop_loss,omitempty"`
	RequestedQty int       `json:"requested_qty,omitempty"`
	CashAtCheck  float64   `json:"cash_at_check,omitempty"`
	TradingDate  string    `json:"trading_date"`
	RejectedAt   time.Time `json:"rejected_at"`
}

func (t ClosedTrade) effectiveStepPrice() float64 {
	if t.StepPriceValue > 0 {
		return t.StepPriceValue
	}
	return 1
}

// LotValueRub — стоимость одного лота в рублях на цене входа.
func (t ClosedTrade) LotValueRub() float64 {
	return t.EntryPrice * t.effectiveStepPrice()
}

// NotionalRub — общая сумма позиции на входе в рублях.
func (t ClosedTrade) NotionalRub() float64 {
	return t.LotValueRub() * float64(t.Quantity)
}
