// Package strategy — сигнальные стратегии и общий слой вокруг них.
//
// Файлы пакета делятся на две группы.
//
// # Сигнальные стратегии (14 файлов, 16 зарегистрированных ID)
//
// Каждый файл — одна торговая идея: реализует strategy.CandleStrategy
// (ID + OnCandle(candle) *Order — «войти сейчас?» и куда SL/TP) и
// регистрируется в своём init() через Register. Позицию, сайзинг, выход,
// трейлинг, риск и запись сделки стратегия НЕ трогает — это делает обёртка
// selfmanaged.SelfManagedStrategy (подпакет ./selfmanaged, ADR 0001).
//
//	momentum.go                      прорыв локальных high/low за lookback
//	momentum_filtered.go             momentum breakout + фильтры режима, настраиваемый RR
//	momentum_sber_daytrend.go        momentum_filtered + фильтр направления дневного тренда (только SBER)
//	opening_range.go                 диапазон открытия
//	opening_range_continuation.go    продолжение после диапазона открытия (+ вариант session_orc)
//	opening_range_fade.go            откат от диапазона открытия (+ вариант session_or_fade)
//	mean_reversion.go                возврат к среднему
//	vwap_pullback_continuation.go    направление OR breakout + откат к VWAP
//	midday_compression_breakout.go   сжатие ATR около полудня + пробой без ретеста
//	late_session_imbalance.go        объёмный дисбаланс в конце сессии по дневному тренду
//	prev_day_level_breakout.go       пробой high/low предыдущего торгового дня
//	afternoon_range_fade.go          fade ложного пробоя послеобеденного диапазона (зеркало OR Fade)
//	session_gap_drive.go             вход по направлению gap относительно close предыдущего дня
//	random_entry.go                  контрольная: случайный вход (baseline «бьём ли монетку»)
//
// # Общий слой (7 файлов)
//
// Код, общий для всех стратегий, плюс реестр, через который optimizer и
// backtest находят и собирают стратегию по ID. Держится в одном пакете
// намеренно: стратегии в init() зовут Register, а Register нужен тип
// Descriptor со ссылкой на CandleStrategy — разнос по подпакетам дал бы
// цикл импортов (прагматика важнее чистоты, см. глобальный CLAUDE.md).
//
//	candlestrategy.go  интерфейс CandleStrategy, все константы ID*, DefaultRewardRatio
//	common.go          общий код стратегий: candleBuffer (прогрев), геометрия
//	                   стопа/тейка (calcStopTP, stopDistance), commonStopOpts, разбор параметров
//	options.go         константы режимов (StopModeRange/StopModeATR) и дефолты ATR/volume
//	params.go          тип Params map[string]float64 + аксессоры — вход random search оптимизатора
//	registry.go        Descriptor / Register / Get / ResolveType — реестр стратегий
//	search_space.go    резолв пути к YAML search-space стратегии
//	snapshot.go        ParamsSnapshot — JSON-снимок параметров trial, привязывается к сделке в backtest/export
//
// Настоящая граница движок ↔ стратегия вынесена отдельно: leaf-пакет
// internal/engine/contract (см. docs/architecture.md и ADR 0001).
package strategy
