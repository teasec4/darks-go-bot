package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"dabkrsbot/internal/config"
	"dabkrsbot/internal/handler"
	"dabkrsbot/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Ошибка загрузки конфига: %v", err)
	}

	if cfg.TGKey == "" {
		log.Fatal("TG_KEY не установлен в переменных окружения или .env файле")
	}

	// Инициализация SQLite
	store, err := storage.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Ошибка инициализации БД: %v", err)
	}
	defer store.Close()
	log.Printf("БД подключена: %s", cfg.DBPath)

	bot, err := tgbotapi.NewBotAPI(cfg.TGKey)
	if err != nil {
		log.Fatalf("Ошибка инициализации бота: %v", err)
	}

	log.Printf("Бот @%s запущен", bot.Self.UserName)
	if cfg.AdminID != 0 {
		log.Printf("Администратор: %d", cfg.AdminID)
	} else {
		log.Println("ВНИМАНИЕ: ADMIN_ID не задан — пересылка работать не будет")
	}

	bot.Debug = false

	h := handler.New(bot, cfg.AdminID, store)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Получен сигнал завершения, останавливаем бота...")
		bot.StopReceivingUpdates()
		store.Close()
	}()

	for update := range updates {
		h.Handle(update)
	}

	log.Println("Бот остановлен")
}
