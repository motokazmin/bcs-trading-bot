package contract

// Alerter — тревога человеку (Telegram), а не строка в логе: то, что требует
// действия руками. 2026-10-11 протухший refresh token ~32 ч держал бот без
// данных, а в логе было 1908 строк, которые никто не читал.
// Реализация — internal/engine/notify; Alert не блокирует вызывающего.
type Alerter interface {
	Alert(msg string)
}
