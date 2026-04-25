package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	TGKey   string
	AdminID int64
	DBPath  string
}

func Load() (*Config, error) {
	// Пробуем загрузить .env, но не падаем если его нет (в контейнере могут быть только переменные окружения)
	_ = godotenv.Load()

	adminID := int64(0)
	if v := getEnv("ADMIN_ID", ""); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return &Config{}, err
		}
		adminID = id
	}

	cfg := &Config{
		TGKey:   getEnv("TG_KEY", ""),
		AdminID: adminID,
		DBPath:  getEnv("DB_PATH", "data/bot.db"),
	}

	return cfg, nil
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value != "" {
		return value
	}
	return fallback
}
