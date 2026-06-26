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

	handler := admin.NewHandler(store)
	server, err := admin.NewServer(cfg, handler)
	if err != nil {
		log.Fatalf("сервер: %v", err)
	}

	log.Printf("админка: http://%s (БД: %s)", cfg.Listen, cfg.DBPath)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
