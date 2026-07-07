package sqlite

import (
	"context"
	"fmt"

	"bcs-trading-bot/pkg/models"
)

func (s *Store) ListClosedTrades(ctx context.Context, f models.TradeFilter, limit, offset int) (models.TradeListResult, error) {
	where, args := buildWhere(f)

	var total int
	countSQL := "SELECT COUNT(*) FROM closed_trades " + where
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return models.TradeListResult{}, fmt.Errorf("count trades: %w", err)
	}

	if limit <= 0 {
		limit = 50
	}
	query := fmt.Sprintf(
		"SELECT %s FROM closed_trades %s ORDER BY closed_at DESC LIMIT ? OFFSET ?",
		closedTradeSelectCols, where,
	)
	queryArgs := append(append([]any{}, args...), limit, offset)

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return models.TradeListResult{}, fmt.Errorf("list trades: %w", err)
	}
	defer rows.Close()

	var trades []models.ClosedTrade
	for rows.Next() {
		tr, err := scanClosedTrade(rows)
		if err != nil {
			return models.TradeListResult{}, err
		}
		trades = append(trades, tr)
	}
	if err := rows.Err(); err != nil {
		return models.TradeListResult{}, err
	}

	return models.TradeListResult{Trades: trades, Total: total}, nil
}

func (s *Store) GetSummary(ctx context.Context, f models.TradeFilter) (models.TradeSummary, error) {
	where, args := buildWhere(f)
	query := `
		SELECT
			COUNT(*) AS trade_count,
			COALESCE(SUM(CASE WHEN is_winner = 1 THEN 1 ELSE 0 END), 0) AS win_count,
			COALESCE(SUM(gross_pnl), 0) AS total_pnl,
			COALESCE(AVG(gross_pnl), 0) AS avg_pnl,
			COALESCE(AVG(pnl_r), 0) AS avg_pnl_r,
			COALESCE(AVG(hold_seconds), 0) AS avg_hold,
			COALESCE(MAX(gross_pnl), 0) AS best_pnl,
			COALESCE(MIN(gross_pnl), 0) AS worst_pnl,
			COALESCE(SUM(CASE WHEN gross_pnl > 0 THEN gross_pnl ELSE 0 END), 0) AS gross_wins,
			COALESCE(SUM(CASE WHEN gross_pnl < 0 THEN gross_pnl ELSE 0 END), 0) AS gross_losses,
			COALESCE(AVG(CASE WHEN gross_pnl > 0 THEN gross_pnl END), 0) AS avg_win,
			COALESCE(AVG(CASE WHEN gross_pnl < 0 THEN gross_pnl END), 0) AS avg_loss
		FROM closed_trades ` + where

	var (
		summary models.TradeSummary
		grossWins, grossLosses, avgWin, avgLoss float64
	)
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&summary.TradeCount,
		&summary.WinCount,
		&summary.TotalPnL,
		&summary.AvgPnL,
		&summary.AvgPnLR,
		&summary.AvgHoldSec,
		&summary.BestTradePnL,
		&summary.WorstTradePnL,
		&grossWins,
		&grossLosses,
		&avgWin,
		&avgLoss,
	)
	if err != nil {
		return models.TradeSummary{}, fmt.Errorf("summary: %w", err)
	}

	summary.LossCount = summary.TradeCount - summary.WinCount
	if summary.TradeCount > 0 {
		summary.WinRate = float64(summary.WinCount) / float64(summary.TradeCount) * 100
	}
	if grossLosses < 0 {
		summary.ProfitFactor = grossWins / (-grossLosses)
	}
	winRate := 0.0
	lossRate := 0.0
	if summary.TradeCount > 0 {
		winRate = float64(summary.WinCount) / float64(summary.TradeCount)
		lossRate = float64(summary.LossCount) / float64(summary.TradeCount)
	}
	summary.Expectancy = winRate*avgWin + lossRate*avgLoss
	summary.ExpectancyR = summary.AvgPnLR

	return summary, nil
}

func (s *Store) GetBreakdown(ctx context.Context, f models.TradeFilter, groupBy string) ([]models.BreakdownRow, error) {
	col, err := breakdownColumn(groupBy)
	if err != nil {
		return nil, err
	}

	where, args := buildWhere(f)
	query := fmt.Sprintf(`
		SELECT
			%s AS grp,
			COUNT(*) AS trade_count,
			COALESCE(SUM(CASE WHEN is_winner = 1 THEN 1 ELSE 0 END), 0) AS win_count,
			COALESCE(SUM(gross_pnl), 0) AS total_pnl,
			COALESCE(AVG(pnl_r), 0) AS avg_pnl_r,
			COALESCE(SUM(CASE WHEN gross_pnl > 0 THEN gross_pnl ELSE 0 END), 0) AS gross_wins,
			COALESCE(SUM(CASE WHEN gross_pnl < 0 THEN gross_pnl ELSE 0 END), 0) AS gross_losses
		FROM closed_trades %s
		GROUP BY grp
		ORDER BY total_pnl DESC`, col, where)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("breakdown: %w", err)
	}
	defer rows.Close()

	var out []models.BreakdownRow
	for rows.Next() {
		var (
			row        models.BreakdownRow
			winCount   int
			grossWins  float64
			grossLosses float64
		)
		if err := rows.Scan(&row.Key, &row.TradeCount, &winCount, &row.TotalPnL, &row.AvgPnLR, &grossWins, &grossLosses); err != nil {
			return nil, err
		}
		if row.TradeCount > 0 {
			row.WinRate = float64(winCount) / float64(row.TradeCount) * 100
		}
		if grossLosses < 0 {
			row.ProfitFactor = grossWins / (-grossLosses)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func breakdownColumn(groupBy string) (string, error) {
	switch groupBy {
	case "experiment_id", "ticker", "close_reason", "trading_date", "stop_mode", "trading_mode":
		return groupBy, nil
	default:
		return "", fmt.Errorf("неподдерживаемая группировка %q", groupBy)
	}
}

func (s *Store) GetDailyPnL(ctx context.Context, f models.TradeFilter) ([]models.DailyPnLRow, error) {
	where, args := buildWhere(f)
	query := `
		SELECT
			trading_date,
			COUNT(*) AS trade_count,
			COALESCE(SUM(gross_pnl), 0) AS total_pnl,
			COALESCE(SUM(CASE WHEN is_winner = 1 THEN 1 ELSE 0 END), 0) AS win_count
		FROM closed_trades ` + where + `
		GROUP BY trading_date
		ORDER BY trading_date`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("daily pnl: %w", err)
	}
	defer rows.Close()

	var out []models.DailyPnLRow
	for rows.Next() {
		var row models.DailyPnLRow
		var winCount int
		if err := rows.Scan(&row.TradingDate, &row.TradeCount, &row.TotalPnL, &winCount); err != nil {
			return nil, err
		}
		if row.TradeCount > 0 {
			row.WinRate = float64(winCount) / float64(row.TradeCount) * 100
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) GetEquityCurve(ctx context.Context, f models.TradeFilter) ([]models.EquityPoint, error) {
	where, args := buildWhere(f)
	query := `
		SELECT closed_at, gross_pnl
		FROM closed_trades ` + where + `
		ORDER BY closed_at ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("equity curve: %w", err)
	}
	defer rows.Close()

	var out []models.EquityPoint
	var cumulative float64
	for rows.Next() {
		var closedAt string
		var pnl float64
		if err := rows.Scan(&closedAt, &pnl); err != nil {
			return nil, err
		}
		ts, err := parseDBTime(closedAt)
		if err != nil {
			return nil, err
		}
		cumulative += pnl
		out = append(out, models.EquityPoint{ClosedAt: ts, CumulativePnL: cumulative})
	}
	return out, rows.Err()
}

func (s *Store) GetDateRange(ctx context.Context, f models.TradeFilter) (models.DateRange, error) {
	where, args := buildWhere(f)
	query := `SELECT COALESCE(MIN(trading_date), ''), COALESCE(MAX(trading_date), '') FROM closed_trades ` + where

	var dr models.DateRange
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&dr.From, &dr.To)
	if err != nil {
		return models.DateRange{}, fmt.Errorf("date range: %w", err)
	}
	return dr, nil
}

func (s *Store) ListExperimentIDs(ctx context.Context, f models.TradeFilter) ([]string, error) {
	where, args := buildWhere(f)
	query := `SELECT DISTINCT experiment_id FROM closed_trades ` + where + ` ORDER BY experiment_id`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list experiments: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListAllClosedTrades возвращает все сделки по фильтру (для экспорта).
func (s *Store) ListAllClosedTrades(ctx context.Context, f models.TradeFilter) ([]models.ClosedTrade, error) {
	result, err := s.ListClosedTrades(ctx, f, 1_000_000, 0)
	if err != nil {
		return nil, err
	}
	return result.Trades, nil
}
