package main

import (
	"log"

	"bcs-trading-bot/internal/admin"
	"bcs-trading-bot/internal/storage/sqlite"
)

func main() {
	cfg := admin.LoadConfig()

	store, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("открытие БД %q: %v", cfg.DBPath, err)
	}
	defer store.Close()

	archives := admin.NewArchiveStore(cfg.ArchivesPath)
	handler := admin.NewHandler(store, archives, cfg.BotLiveURL)
	server, err := admin.NewServer(cfg, handler)
	if err != nil {
		log.Fatalf("сервер: %v", err)
	}

	log.Printf("админка: http://%s (БД: %s, bot-live: %s)", cfg.Listen, cfg.DBPath, cfg.BotLiveURL)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
