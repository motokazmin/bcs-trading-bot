package admin

import (
	"flag"
	"time"
)

// Config — параметры запуска админки.
type Config struct {
	DBPath       string
	ArchivesPath string
	Listen       string
}

func LoadConfig() Config {
	db := flag.String("db", "data/trades.db", "путь к SQLite с закрытыми сделками")
	archives := flag.String("archives", "data/archives.json", "путь к JSON с архивами периодов")
	listen := flag.String("listen", "127.0.0.1:8090", "адрес HTTP-сервера")
	flag.Parse()
	return Config{
		DBPath:       *db,
		ArchivesPath: *archives,
		Listen:       *listen,
	}
}

func (c Config) ReadTimeout() time.Duration  { return 10 * time.Second }
func (c Config) WriteTimeout() time.Duration { return 60 * time.Second }
