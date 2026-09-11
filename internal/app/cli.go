// Package app — composition root бота: разбор флагов, поднятие технических
// зависимостей (брокер, хранилище, портфельный риск, исполнитель) и сборка
// торгового набора (эксперимент × тикер) в запускаемый Trader.
//
// cmd/bot/main.go читается как оглавление: каждая строка — вызов одной
// функции этого пакета. Вся «простыня» деталей — здесь, по файлам:
//   - cli.go          — флаги CLI
//   - logging.go      — инициализация логов
//   - broker.go       — OAuth + подключение к БКС
//   - dependencies.go — хранилище, портфельный риск, исполнитель ордеров
//   - trader.go       — цикл experiment × ticker, раннеры, DataFeed
//   - dashboard.go    — HTTP UI/API админки
//   - smoke.go        — smoke-test
package app

import (
	"flag"
	"os"
	"strings"
)

// Options — разобранные флаги и переменные окружения запуска бота.
type Options struct {
	ConfigPath   string
	NoColor      bool
	SmokeTest    bool
	HTTPListen   string
	ArchivesPath string

	// LogPath — путь к лог-файлу после нормализации ("" / "-" → только stdout).
	LogPath string
}

// ParseFlags разбирает os.Args. Дефолт лог-файла берётся из LOG_FILE, иначе
// /var/log/trading-bot/bot.log.
func ParseFlags() Options {
	defaultLogFile := "/var/log/trading-bot/bot.log"
	if v := strings.TrimSpace(os.Getenv("LOG_FILE")); v != "" {
		defaultLogFile = v
	}

	configPath := flag.String("config", "configs/runs/portfolio-paper.yaml", "путь к YAML-конфигу")
	noColor := flag.Bool("no-color", false, "отключить цветной вывод в терминале")
	logFile := flag.String("log-file", defaultLogFile, "лог в файл + stdout (дефолт /var/log/trading-bot/bot.log; пустая строка или \"-\" — только stdout)")
	smokeTest := flag.Bool("smoke-test", false, "быстрая проверка: OAuth + WebSocket + виртуальная сделка без записи в БД")
	httpListen := flag.String("http-listen", "127.0.0.1:8091", "адрес HTTP UI/API админки (пустая строка — отключить)")
	archivesPath := flag.String("archives", "data/archives.json", "путь к JSON с архивами периодов")
	flag.Parse()

	logPath := strings.TrimSpace(*logFile)
	if logPath == "-" {
		logPath = ""
	}

	return Options{
		ConfigPath:   *configPath,
		NoColor:      *noColor,
		SmokeTest:    *smokeTest,
		HTTPListen:   *httpListen,
		ArchivesPath: *archivesPath,
		LogPath:      logPath,
	}
}
