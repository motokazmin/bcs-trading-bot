// Package logx — цветной структурированный вывод в терминал.
package logx

import (
	"fmt"
	"io"
	"log"
	"os"
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

// SetOutput задаёт writer для логов (тесты).
func SetOutput(w io.Writer) {
	out = log.New(w, "", 0)
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
	out.Println(paint(dim, time.Now().Format("15:04:05")) + " " + msg)
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
