package handler

import (
	"fmt"
	"log"
	"strings"
	"time"

	"dabkrsbot/internal/ratelimit"
	"dabkrsbot/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Handler struct {
	bot         *tgbotapi.BotAPI
	adminID     int64
	rl          *ratelimit.RateLimiter
	store       *storage.Storage
	stopCleanup chan struct{}
	stats       *Stats
}

type Stats struct {
	totalMessages int
	blockedCount  int
	forwardedCount int
	startedAt     time.Time
}

func New(bot *tgbotapi.BotAPI, adminID int64, store *storage.Storage) *Handler {
	rl := ratelimit.New(3, time.Hour)

	// Фоновая чистка старых записей rate limiter — раз в 15 минут
	stopCleanup := make(chan struct{})
	rl.StartCleanup(15*time.Minute, stopCleanup)

	return &Handler{
		bot:         bot,
		adminID:     adminID,
		rl:          rl,
		store:       store,
		stopCleanup: stopCleanup,
		stats: &Stats{
			startedAt: time.Now(),
		},
	}
}

func (h *Handler) Handle(update tgbotapi.Update) {
	msg := update.Message
	if msg == nil {
		return
	}

	h.stats.totalMessages++

	user := msg.From
	log.Printf("[%s] %s (ID: %d): %s", user.UserName, user.FirstName, user.ID, msg.Text)

	// Если сообщение от админа — команды
	if user.ID == h.adminID {
		h.handleAdminCommand(msg)
		return
	}

	// Команды пользователя (не тратят лимит)
	if strings.HasPrefix(msg.Text, "/") {
		if h.handleUserCommand(msg) {
			return
		}
	}

	// Проверка блокировки через БД
	blocked, err := h.store.IsBlocked(user.ID)
	if err != nil {
		log.Printf("Ошибка проверки блокировки: %v", err)
	}
	if blocked {
		h.stats.blockedCount++
		log.Printf("Заблокированный пользователь %d попытался отправить сообщение", user.ID)
		return
	}

	// Rate limit
	remaining := h.rl.Remaining(user.ID)
	if !h.rl.Allow(user.ID) {
		h.stats.blockedCount++
		log.Printf("Rate limit превышен для пользователя %d", user.ID)
		h.reply(msg.Chat.ID,
			fmt.Sprintf("Вы превысили лимит сообщений. Подождите, пожалуйста. Лимит: %d сообщ. в час.", h.rl.ResetInterval()/time.Hour),
		)
		return
	}

	// Сохраняем в БД
	if err := h.store.SaveFeedback(user.ID, user.UserName, user.FirstName, msg.Text); err != nil {
		log.Printf("Ошибка сохранения отзыва: %v", err)
	}

	// Пересылаем админу
	h.forwardToAdmin(msg, remaining-1)
	// Подтверждаем пользователю
	h.confirmUser(msg.Chat.ID, remaining-1)
}

func (h *Handler) reply(chatID int64, text string) {
	rmsg := tgbotapi.NewMessage(chatID, text)
	rmsg.ParseMode = "HTML"
	if _, err := h.bot.Send(rmsg); err != nil {
		log.Printf("Ошибка отправки сообщения: %v", err)
	}
}

func (h *Handler) confirmUser(chatID int64, remaining int) {
	text := "✅ Ваш отзыв получен! Спасибо, что помогаете сделать сервис лучше.\n\n"
	if remaining > 0 {
		text += fmt.Sprintf("Осталось сообщений: %d", remaining)
	} else {
		text += "Это было ваше последнее сообщение в этом часе."
	}
	h.reply(chatID, text)
}

func (h *Handler) handleUserCommand(msg *tgbotapi.Message) bool {
	cmd := strings.Fields(msg.Text)[0]

	switch cmd {
	case "/start":
		h.reply(msg.Chat.ID,
			"👋 <b>Добро пожаловать!</b>\n\n"+
				"Это бот обратной связи для сервиса перевода с китайского на русский.\n\n"+
				"Если вы нашли ошибку в переводе, заметили неточность или хотите "+
				"предложить улучшение — просто напишите сообщение, и я передам его разработчику.\n\n"+
				"📌 <b>Лимит:</b> 3 сообщения в час",
		)
		return true

	case "/info":
		h.reply(msg.Chat.ID,
			"ℹ️ <b>О сервисе</b>\n\n"+
				"Сервис перевода слов с китайского языка на русский.\n\n"+
				"Нашли ошибку? Напишите её текстом — мы всё поправим.\n\n"+
				"💬 Команды:\n"+
				"<code>/start</code> — приветствие\n"+
				"<code>/info</code> — информация о боте",
		)
		return true

	default:
		return false
	}
}

func (h *Handler) forwardToAdmin(msg *tgbotapi.Message, remaining int) {
	if h.adminID == 0 {
		log.Println("ADMIN_ID не задан, пересылка невозможна")
		return
	}

	user := msg.From
	text := fmt.Sprintf("📩 <b>Новый отзыв</b>\n\n%s\n\n", msg.Text)
	text += fmt.Sprintf("👤 <b>От:</b> %s %s\n", user.FirstName, user.LastName)
	if user.UserName != "" {
		text += fmt.Sprintf("🔗 @%s\n", user.UserName)
	}
	text += fmt.Sprintf("🆔 ID: <code>%d</code>\n", user.ID)
	if user.LanguageCode != "" {
		text += fmt.Sprintf("🌐 Язык: %s\n", user.LanguageCode)
	}
	text += fmt.Sprintf("\n📊 Осталось сообщений у пользователя: %d", remaining)

	rmsg := tgbotapi.NewMessage(h.adminID, text)
	rmsg.ParseMode = "HTML"
	if _, err := h.bot.Send(rmsg); err != nil {
		log.Printf("Ошибка пересылки админу: %v", err)
	} else {
		h.stats.forwardedCount++
	}
}

func (h *Handler) handleAdminCommand(msg *tgbotapi.Message) {
	parts := strings.Fields(msg.Text)
	if len(parts) == 0 {
		return
	}

	cmd := parts[0]

	switch cmd {
	case "/start":
		h.reply(msg.Chat.ID,
			"👋 <b>Бот сбора отзывов запущен</b>\n\n"+
				"Ты будешь получать все сообщения от пользователей.\n\n"+
				"<b>Команды:</b>\n"+
				"<code>/stats</code> — статистика\n"+
				"<code>/block &lt;user_id&gt;</code> — заблокировать пользователя\n"+
				"<code>/unblock &lt;user_id&gt;</code> — разблокировать\n"+
				"<code>/blocklist</code> — список заблокированных",
		)

	case "/stats":
		// Статистика из БД + сессионная
		dbStats, err := h.store.GetStats()
		if err != nil {
			log.Printf("Ошибка получения статистики из БД: %v", err)
			h.reply(msg.Chat.ID, "Ошибка получения статистики")
			return
		}

		h.reply(msg.Chat.ID, fmt.Sprintf(
			"📊 <b>Статистика бота</b>\n\n"+
				"<u>За всё время (БД):</u>\n"+
				"Всего отзывов: %d\n"+
				"Переслано: %d\n"+
				"Заблокировано пользователей: %d\n"+
				"Сбор с: %s\n\n"+
				"<u>С текущего запуска:</u>\n"+
				"Сообщений получено: %d\n"+
				"Отклонено (спам): %d\n"+
				"Переслано: %d\n",
			dbStats.TotalFeedbacks,
			dbStats.Forwarded,
			dbStats.BlockedUsers,
			dbStats.Since.Format("2006-01-02 15:04"),
			h.stats.totalMessages,
			h.stats.blockedCount,
			h.stats.forwardedCount,
		))

	case "/block":
		if len(parts) < 2 {
			h.reply(msg.Chat.ID, "Укажите ID пользователя: <code>/block 123456789</code>")
			return
		}
		userID := parseInt64(parts[1])
		if userID == 0 {
			h.reply(msg.Chat.ID, "Некорректный ID")
			return
		}
		reason := ""
		if len(parts) > 2 {
			reason = strings.Join(parts[2:], " ")
		}
		if err := h.store.BlockUser(userID, reason); err != nil {
			log.Printf("Ошибка блокировки: %v", err)
			h.reply(msg.Chat.ID, "Ошибка при блокировке")
			return
		}
		h.reply(msg.Chat.ID, fmt.Sprintf("🚫 Пользователь <code>%d</code> заблокирован.", userID))

	case "/unblock":
		if len(parts) < 2 {
			h.reply(msg.Chat.ID, "Укажите ID пользователя: <code>/unblock 123456789</code>")
			return
		}
		userID := parseInt64(parts[1])
		if userID == 0 {
			h.reply(msg.Chat.ID, "Некорректный ID")
			return
		}
		if err := h.store.UnblockUser(userID); err != nil {
			log.Printf("Ошибка разблокировки: %v", err)
			h.reply(msg.Chat.ID, "Ошибка при разблокировке")
			return
		}
		h.reply(msg.Chat.ID, fmt.Sprintf("✅ Пользователь <code>%d</code> разблокирован.", userID))

	case "/blocklist":
		users, err := h.store.GetAllBlocked()
		if err != nil {
			log.Printf("Ошибка получения блокировок: %v", err)
			h.reply(msg.Chat.ID, "Ошибка получения списка")
			return
		}
		if len(users) == 0 {
			h.reply(msg.Chat.ID, "📋 Список заблокированных пуст.")
			return
		}
		var sb strings.Builder
		sb.WriteString("📋 <b>Заблокированные пользователи:</b>\n\n")
		for id, reason := range users {
			sb.WriteString(fmt.Sprintf("🆔 <code>%d</code>", id))
			if reason != "" {
				sb.WriteString(fmt.Sprintf(" — %s", reason))
			}
			sb.WriteString("\n")
		}
		h.reply(msg.Chat.ID, sb.String())

	default:
		h.reply(msg.Chat.ID, "Неизвестная команда. Используй /start для списка команд.")
	}
}

func parseInt64(s string) int64 {
	var id int64
	_, _ = fmt.Sscanf(s, "%d", &id)
	return id
}
