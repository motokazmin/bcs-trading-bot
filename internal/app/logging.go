package app

import (
	"fmt"
	"os"

	"bcs-trading-bot/internal/config"
	"bcs-trading-bot/internal/logx"
)

// SetupLogging настраивает вывод логов по Options и возвращает функцию
// завершения (закрытие лог-файла) для defer в main. При ошибке открытия
// файла — os.Exit(1).
func SetupLogging(opts Options) func() {
	shutdown := func() {}
	if opts.LogPath != "" {
		closer, err := logx.OpenFile(opts.LogPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "log-file %s: %v\n", opts.LogPath, err)
			os.Exit(1)
		}
		shutdown = func() { _ = closer.Close() }
	} else if opts.NoColor {
		logx.SetColorEnabled(false)
	}

	logx.Info("Запуск торгового робота БКС на Go...")
	if opts.LogPath != "" {
		logx.Info("Лог-файл: %s (stdout + файл)", opts.LogPath)
	}
	return shutdown
}

// MustLoadConfig загружает YAML-конфиг; при ошибке — logx.Fatalf.
func MustLoadConfig(path string) *config.Config {
	cfg, err := config.Load(path)
	if err != nil {
		logx.Fatalf("ошибка загрузки конфига: %v", err)
	}
	return cfg
}
