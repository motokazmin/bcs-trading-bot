package sqlite

import (
	"fmt"
	"strings"
	"time"

	"bcs-trading-bot/internal/models"
)

func buildWhere(f models.TradeFilter) (string, []any) {
	var clauses []string
	var args []any

	if f.ExperimentID != "" {
		clauses = append(clauses, "experiment_id = ?")
		args = append(args, f.ExperimentID)
	}
	if f.Ticker != "" {
		clauses = append(clauses, "ticker = ?")
		args = append(args, f.Ticker)
	}
	if f.TradingMode != "" {
		clauses = append(clauses, "trading_mode = ?")
		args = append(args, f.TradingMode)
	}
	if f.RunID != "" {
		clauses = append(clauses, "run_id = ?")
		args = append(args, f.RunID)
	}
	if f.CloseReason != "" {
		clauses = append(clauses, "close_reason = ?")
		args = append(args, f.CloseReason)
	}
	if f.DateFrom != "" {
		clauses = append(clauses, "trading_date >= ?")
		args = append(args, f.DateFrom)
	}
	if f.DateTo != "" {
		clauses = append(clauses, "trading_date <= ?")
		args = append(args, f.DateTo)
	}
	// Архивные периоды: вырезаем целиком, чтобы их не было ни в списке, ни в агрегатах.
	for _, ex := range f.ExcludeRanges {
		switch {
		case ex.From != "" && ex.To != "":
			clauses = append(clauses, "NOT (trading_date >= ? AND trading_date <= ?)")
			args = append(args, ex.From, ex.To)
		case ex.From != "":
			clauses = append(clauses, "trading_date < ?")
			args = append(args, ex.From)
		case ex.To != "":
			clauses = append(clauses, "trading_date > ?")
			args = append(args, ex.To)
		}
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

const closedTradeSelectCols = `
	id, trading_mode, run_id, experiment_id, stop_mode, recorded_at,
	ticker, class_code, step_price_value,
	direction, quantity,
	entry_price, exit_price,
	initial_stop_loss, initial_take_profit, final_stop_loss, r_distance,
	gross_pnl, pnl_r, mfe_in_r, mae_in_r, breakout_upper, breakout_lower,
	close_reason, trail_stage, is_winner,
	opened_at, closed_at, hold_seconds, trading_date,
	candle_timeframe, lookback, risk_per_trade_pct, deposit_per_ticker,
	COALESCE(audit_severity, ''), COALESCE(audit_codes, ''),
	COALESCE(entry_bar_time, ''), COALESCE(entry_bar_close, 0)
`

func scanClosedTrade(scanner interface {
	Scan(dest ...any) error
}) (models.ClosedTrade, error) {
	var (
		id               int
		tradingMode      string
		runID            string
		experimentID     string
		stopMode         string
		recordedAt       string
		ticker           string
		classCode        string
		stepPriceValue   float64
		direction        string
		quantity         int
		entryPrice       float64
		exitPrice        float64
		initialStopLoss  float64
		initialTakeProfit float64
		finalStopLoss    float64
		rDistance        float64
		grossPnL         float64
		pnlR             float64
		mfeInR           float64
		maeInR           float64
		breakoutUpper    float64
		breakoutLower    float64
		closeReason      string
		trailStage       int
		isWinner         int
		openedAt         string
		closedAt         string
		holdSeconds      int
		tradingDate      string
		candleTimeframe  string
		lookback         int
		riskPerTradePct  float64
		depositPerTicker float64
		auditSeverity    string
		auditCodes       string
		entryBarTime     string
		entryBarClose    float64
	)

	err := scanner.Scan(
		&id, &tradingMode, &runID, &experimentID, &stopMode, &recordedAt,
		&ticker, &classCode, &stepPriceValue,
		&direction, &quantity,
		&entryPrice, &exitPrice,
		&initialStopLoss, &initialTakeProfit, &finalStopLoss, &rDistance,
		&grossPnL, &pnlR, &mfeInR, &maeInR, &breakoutUpper, &breakoutLower,
		&closeReason, &trailStage, &isWinner,
		&openedAt, &closedAt, &holdSeconds, &tradingDate,
		&candleTimeframe, &lookback, &riskPerTradePct, &depositPerTicker,
		&auditSeverity, &auditCodes, &entryBarTime, &entryBarClose,
	)
	if err != nil {
		return models.ClosedTrade{}, err
	}

	opened, err := parseDBTime(openedAt)
	if err != nil {
		return models.ClosedTrade{}, fmt.Errorf("opened_at: %w", err)
	}
	closed, err := parseDBTime(closedAt)
	if err != nil {
		return models.ClosedTrade{}, fmt.Errorf("closed_at: %w", err)
	}

	return models.ClosedTrade{
		TradingMode:       tradingMode,
		RunID:             runID,
		ExperimentID:      experimentID,
		StopMode:          stopMode,
		Ticker:            ticker,
		ClassCode:         classCode,
		StepPriceValue:    stepPriceValue,
		Direction:         direction,
		Quantity:          quantity,
		EntryPrice:        entryPrice,
		ExitPrice:         exitPrice,
		InitialStopLoss:   initialStopLoss,
		InitialTakeProfit: initialTakeProfit,
		FinalStopLoss:     finalStopLoss,
		RDistance:         rDistance,
		GrossPnL:          grossPnL,
		PnLR:              pnlR,
		MFEinR:            mfeInR,
		MAEinR:            maeInR,
		BreakoutUpper:     breakoutUpper,
		BreakoutLower:     breakoutLower,
		CloseReason:       closeReason,
		TrailStage:        trailStage,
		IsWinner:          isWinner != 0,
		OpenedAt:          opened,
		ClosedAt:          closed,
		HoldSeconds:       holdSeconds,
		TradingDate:       tradingDate,
		CandleTimeframe:   candleTimeframe,
		Lookback:          lookback,
		RiskPerTradePct:   riskPerTradePct,
		DepositPerTicker:  depositPerTicker,
		AuditSeverity:     auditSeverity,
		AuditCodes:        auditCodes,
		EntryBarTime:      entryBarTime,
		EntryBarClose:     entryBarClose,
	}, nil
}

func parseDBTime(value string) (time.Time, error) {
	t, err := time.ParseInLocation(timeLayout, value, dbLoc)
	if err != nil {
		return time.ParseInLocation("2006-01-02 15:04:05.999999999", value, dbLoc)
	}
	return t, nil
}
