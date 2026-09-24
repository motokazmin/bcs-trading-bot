// Package logx — цветной структурированный вывод в терминал.
package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
)

const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
)

var (
	mu           sync.RWMutex
	colorEnabled = detectColor()
	out          = log.New(os.Stdout, "", 0)
)

// moscowLoc — единая зона отметок времени в логах (совпадает с тем, что пишется в БД),
// чтобы вывод не зависел от таймзоны процесса/окружения.
var moscowLoc = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Moscow"); err == nil {
		return loc
	}
	return time.FixedZone("MSK", 3*3600)
}()

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isatty.IsTerminal(os.Stdout.Fd())
}

// SetColorEnabled включает или отключает ANSI-цвета (флаг -no-color).
func SetColorEnabled(enabled bool) {
	mu.Lock()
	colorEnabled = enabled
	mu.Unlock()
}

// SetOutput задаёт writer для логов. nil → os.Stdout (как при старте).
func SetOutput(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	mu.Lock()
	out = log.New(w, "", 0)
	mu.Unlock()
}

// OpenFile открывает path (append/create), пишет одновременно в stdout и файл.
// Цвета отключаются — в файле ANSI не нужны. Создаёт родительские каталоги.
// Закройте возвращённый Closer при завершении процесса.
func OpenFile(path string) (io.Closer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	SetColorEnabled(false)
	SetOutput(io.MultiWriter(os.Stdout, f))
	return f, nil
}

func paint(color, s string) string {
	mu.RLock()
	enabled := colorEnabled
	mu.RUnlock()
	if !enabled || color == "" {
		return s
	}
	return color + s + reset
}

func tag(label, color string) string {
	return paint(color, "["+label+"]")
}

func tickerLabel(ticker string) string {
	if ticker == "" {
		return ""
	}
	return paint(cyan, "["+ticker+"]")
}

// Direction раскрашивает BUY/SELL.
func Direction(d string) string {
	switch d {
	case "BUY":
		return paint(green+bold, d)
	case "SELL":
		return paint(red+bold, d)
	default:
		return d
	}
}

// PnL раскрашивает результат сделки.
func PnL(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	if v > 0 {
		return paint(green+bold, "+"+s)
	}
	if v < 0 {
		return paint(red+bold, s)
	}
	return s
}

func closeReasonTag(reason string) string {
	switch reason {
	case "STOP_LOSS":
		return tag("SL", red+bold)
	case "TAKE_PROFIT":
		return tag("TP", green+bold)
	case "EOD":
		return tag("EOD", yellow+bold)
	case "SMOKE":
		return tag("SMOKE", cyan+bold)
	default:
		return tag(reason, yellow)
	}
}

func write(parts ...string) {
	msg := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if msg != "" {
			msg += " "
		}
		msg += p
	}
	line := paint(dim, time.Now().In(moscowLoc).Format("2006-01-02 15:04:05")) + " " + msg
	mu.RLock()
	l := out
	mu.RUnlock()
	l.Println(line)
}

// Info — обычное системное сообщение.
func Info(format string, args ...interface{}) {
	write(tag("SYS", blue), fmt.Sprintf(format, args...))
}

// Warn — предупреждение.
func Warn(format string, args ...interface{}) {
	write(tag("WARN", yellow+bold), fmt.Sprintf(format, args...))
}

// Error — ошибка.
func Error(format string, args ...interface{}) {
	write(tag("ERR", red+bold), fmt.Sprintf(format, args...))
}

// Fatal — ошибка и выход.
func Fatal(format string, args ...interface{}) {
	Error(format, args...)
	os.Exit(1)
}

// Fatalf — alias для Fatal с форматированием.
func Fatalf(format string, args ...interface{}) {
	Fatal(format, args...)
}

// WS — сообщения WebSocket.
func WS(format string, args ...interface{}) {
	write(tag("WS", magenta), fmt.Sprintf(format, args...))
}

// WorkerLifecycle — старт/стоп воркера.
func WorkerLifecycle(ticker, msg string) {
	write(tickerLabel(ticker), paint(dim, msg))
}

// TradeOpen — открытие позиции.
func TradeOpen(ticker, direction string, qty int, price, sl, tp float64) {
	write(
		tickerLabel(ticker),
		tag("OPEN", green+bold),
		Direction(direction),
		fmt.Sprintf("x%d @ %.2f", qty, price),
		paint(dim, fmt.Sprintf("SL=%.2f TP=%.2f", sl, tp)),
	)
}

// CashCap — кап по кэшу реально урезал объём позиции (даже если сделка всё
// равно открылась) — сигнал конкуренции за общий virtual-баланс. riskQty —
// объём, посчитанный риск-моделью; cappedQty — фактический после капа.
func CashCap(ticker, label string, riskQty, cappedQty int, notional, balance float64) {
	pct := 0.0
	if balance > 0 {
		pct = notional / balance * 100
	}
	write(
		tickerLabel(ticker),
		tag("CASHCAP", yellow),
		paint(dim, label),
		fmt.Sprintf("qty %d→%d notional=%.2f/%.2f (%.1f%%)", riskQty, cappedQty, notional, balance, pct),
	)
}

// SignalRejected — сигнал отклонён.
func SignalRejected(ticker, direction, reason string) {
	write(
		tickerLabel(ticker),
		tag("SKIP", yellow),
		Direction(direction),
		reason,
	)
}

// Trailing — срабатывание трейлинг-стопа.
func Trailing(ticker string, stage int, sl float64) {
	label := fmt.Sprintf("+%dR", stage)
	write(
		tickerLabel(ticker),
		tag("TRAIL", magenta),
		fmt.Sprintf("%s → SL=%.2f", label, sl),
	)
}

// PnLR раскрашивает результат в единицах R.
func PnLR(v float64) string {
	s := fmt.Sprintf("%+.2fR", v)
	if v > 0 {
		return paint(green+bold, s)
	}
	if v < 0 {
		return paint(red+bold, s)
	}
	return s
}

// TradeClose — закрытие позиции.
func TradeClose(ticker, reason string, exitPrice, pnl, pnlR float64) {
	write(
		tickerLabel(ticker),
		closeReasonTag(reason),
		fmt.Sprintf("@ %.2f", exitPrice),
		fmt.Sprintf("PnL=%s", PnL(pnl)),
		PnLR(pnlR),
	)
}

// Audit — нарушение / заметка валидности сделки.
func Audit(ticker, severity, codesCSV string, detail string) {
	color := dim
	switch severity {
	case "error":
		color = red + bold
	case "warn":
		color = yellow
	case "info":
		color = cyan
	}
	msg := fmt.Sprintf("severity=%s", severity)
	if codesCSV != "" {
		msg += " codes=" + codesCSV
	}
	if detail != "" {
		msg += " " + detail
	}
	write(tickerLabel(ticker), tag("AUDIT", color), msg)
}

// DailyReset — сброс дневного счётчика убытков.
func DailyReset(ticker string) {
	write(tickerLabel(ticker), paint(dim, "новый торговый день: дневной счётчик убытков сброшен"))
}

// Mode — режим торговли при старте.
func Mode(virtual bool, detail string) {
	if virtual {
		write(tag("MODE", dim+cyan), "virtual", detail)
	} else {
		write(tag("MODE", red+bold), "REAL", detail)
	}
}
