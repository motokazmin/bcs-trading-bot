package tradeaudit

import (
	"math"
	"strings"
	"time"

	"bcs-trading-bot/pkg/models"
)

const (
	CodeLimitVsClose  = "LIMIT_VS_CLOSE"
	CodeEntryPastStop = "ENTRY_PAST_STOP"
	CodeSameBarSL     = "SAME_BAR_SL"
	CodeSLFillDrift   = "SL_FILL_DRIFT"
	CodeTPFillDrift   = "TP_FILL_DRIFT"
	CodeRRMismatch    = "RR_MISMATCH"
	CodeTrailDead     = "TRAIL_DEAD"
	CodeHoldVsTF      = "HOLD_VS_TF"

	SeverityInfo  = "info"
	SeverityWarn  = "warn"
	SeverityError = "error"

	// LimitDriftWarnR — |entry−close|/R выше этого → warn.
	LimitDriftWarnR = 0.5
	// LimitDriftErrorR — сильный stale limit (TATN-класс).
	LimitDriftErrorR = 1.5
	// FillDriftWarnR — |exit−SL|/R выше этого → warn.
	FillDriftWarnR = 0.25
	// RRMismatchTol — относительный допуск reward_ratio.
	RRMismatchTol = 0.05
)

// OpenInput — данные для проверки входа.
type OpenInput struct {
	Direction     string
	EntryPrice    float64
	StopLoss      float64
	TakeProfit    float64
	RDistance     float64
	BarClose      float64
	LastPrice     float64 // 0 = нет котировки
	RewardRatio   float64 // 0 = не проверять RR_MISMATCH
}

// CloseInput — данные для проверки выхода.
type CloseInput struct {
	Direction       string
	EntryPrice      float64
	ExitPrice       float64
	FinalStopLoss   float64
	TakeProfit      float64
	RDistance       float64
	CloseReason     string
	PnLR            float64
	MFEinR          float64
	TrailStage      int
	TrailActivation float64 // 0 = не проверять TRAIL_DEAD
	HoldSeconds     int
	BarDuration     time.Duration // 0 = M5
	SameBarExit     bool
	EntryBarClose   float64 // для контекста; 0 ок
}

// Result — итог аудита.
type Result struct {
	Severity string
	Codes    []string
	Details  map[string]float64
}

func (r Result) Empty() bool {
	return len(r.Codes) == 0
}

func (r Result) CodesCSV() string {
	return strings.Join(r.Codes, ",")
}

// ValidateOpen проверяет вход относительно бара и стопа.
func ValidateOpen(in OpenInput) Result {
	r := Result{Details: map[string]float64{}}
	if in.RDistance <= 0 || in.EntryPrice <= 0 {
		return r
	}

	if !pricesEqual(in.EntryPrice, in.BarClose) && in.BarClose > 0 {
		drift := math.Abs(in.EntryPrice-in.BarClose) / in.RDistance
		r.Details["limit_vs_close_r"] = drift
		if drift >= LimitDriftErrorR {
			r.add(SeverityError, CodeLimitVsClose)
		} else if drift >= LimitDriftWarnR {
			r.add(SeverityWarn, CodeLimitVsClose)
		}
	}

	if pastStop(in.Direction, in.EntryPrice, in.StopLoss, in.BarClose) ||
		(in.LastPrice > 0 && pastStop(in.Direction, in.EntryPrice, in.StopLoss, in.LastPrice)) {
		r.add(SeverityError, CodeEntryPastStop)
		if in.LastPrice > 0 {
			r.Details["last_vs_entry_r"] = math.Abs(in.LastPrice-in.EntryPrice) / in.RDistance
		}
	}

	if in.RewardRatio > 0 && in.TakeProfit > 0 {
		actual := math.Abs(in.TakeProfit-in.EntryPrice) / in.RDistance
		r.Details["actual_rr"] = actual
		r.Details["configured_rr"] = in.RewardRatio
		if relDiff(actual, in.RewardRatio) > RRMismatchTol {
			r.add(SeverityWarn, CodeRRMismatch)
		}
	}

	return r
}

// ValidateClose проверяет выход относительно уровней и холда.
func ValidateClose(in CloseInput) Result {
	r := Result{Details: map[string]float64{}}
	if in.RDistance <= 0 {
		return r
	}

	if in.SameBarExit && in.CloseReason == models.CloseReasonStopLoss {
		r.add(SeverityInfo, CodeSameBarSL)
	}

	switch in.CloseReason {
	case models.CloseReasonStopLoss:
		if in.FinalStopLoss > 0 {
			drift := math.Abs(in.ExitPrice-in.FinalStopLoss) / in.RDistance
			r.Details["sl_fill_drift_r"] = drift
			if drift > FillDriftWarnR {
				r.add(SeverityWarn, CodeSLFillDrift)
			}
		}
	case models.CloseReasonTakeProfit:
		if in.TakeProfit > 0 {
			drift := math.Abs(in.ExitPrice-in.TakeProfit) / in.RDistance
			r.Details["tp_fill_drift_r"] = drift
			if drift > FillDriftWarnR {
				r.add(SeverityWarn, CodeTPFillDrift)
			}
		}
		if in.TrailStage == 0 && in.TrailActivation > 0 && in.TakeProfit > 0 && in.EntryPrice > 0 {
			tpR := math.Abs(in.TakeProfit-in.EntryPrice) / in.RDistance
			if in.TrailActivation > tpR+1e-9 {
				r.add(SeverityInfo, CodeTrailDead)
				r.Details["trail_activation_r"] = in.TrailActivation
				r.Details["tp_r"] = tpR
			}
		}
	}

	barDur := in.BarDuration
	if barDur <= 0 {
		barDur = 5 * time.Minute
	}
	if in.HoldSeconds > 0 && time.Duration(in.HoldSeconds)*time.Second < barDur && math.Abs(in.PnLR) > 1.5 {
		r.add(SeverityWarn, CodeHoldVsTF)
		r.Details["hold_seconds"] = float64(in.HoldSeconds)
		r.Details["pnl_r"] = in.PnLR
	}

	return r
}

// AnnotateTrade заполняет audit_* поля сделки по open+close проверкам.
func AnnotateTrade(trade *models.ClosedTrade, open OpenInput, close CloseInput) Result {
	audit := Merge(ValidateOpen(open), ValidateClose(close))
	if audit.Empty() {
		return audit
	}
	trade.AuditSeverity = audit.Severity
	trade.AuditCodes = audit.CodesCSV()
	return audit
}

// Merge объединяет open+close аудиты (макс. severity, уникальные коды).
func Merge(parts ...Result) Result {
	out := Result{Details: map[string]float64{}}
	seen := map[string]struct{}{}
	for _, p := range parts {
		for k, v := range p.Details {
			out.Details[k] = v
		}
		for _, c := range p.Codes {
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			out.Codes = append(out.Codes, c)
		}
		out.Severity = maxSeverity(out.Severity, p.Severity)
	}
	return out
}

func (r *Result) add(sev, code string) {
	r.Severity = maxSeverity(r.Severity, sev)
	for _, c := range r.Codes {
		if c == code {
			return
		}
	}
	r.Codes = append(r.Codes, code)
}

func maxSeverity(a, b string) string {
	rank := map[string]int{"": 0, SeverityInfo: 1, SeverityWarn: 2, SeverityError: 3}
	if rank[b] > rank[a] {
		return b
	}
	if a == "" {
		return b
	}
	return a
}

func pastStop(direction string, entry, stop, price float64) bool {
	if price <= 0 || stop <= 0 {
		return false
	}
	switch direction {
	case "BUY":
		return price <= stop
	case "SELL":
		return price >= stop
	default:
		return false
	}
}

func relDiff(a, b float64) float64 {
	if b == 0 {
		return math.Abs(a - b)
	}
	return math.Abs(a-b) / math.Abs(b)
}

func pricesEqual(a, b float64) bool {
	d := math.Abs(a - b)
	if d < 1e-9 {
		return true
	}
	scale := math.Max(math.Abs(a), math.Abs(b))
	return scale > 0 && d/scale < 1e-12
}
