package handler

import (
	"fmt"
	"log"
	"strings"
	"time"

	"dabkrsbot/internal/ratelimit"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Handler обрабатывает сообщения бота.
type Handler struct {
	bot       *tgbotapi.BotAPI
	adminID   int64
	rl        *ratelimit.RateLimiter
	blocklist *ratelimit.Blocklist
	stats     *Stats
}

// Stats — простая статистика.
type Stats struct {
	totalMessages int
	blockedCount  int
	forwardedCount int
	startedAt     time.Time
}

func New(bot *tgbotapi.BotAPI, adminID int64) *Handler {
	return &Handler{
		bot:       bot,
		adminID:   adminID,
		rl:        ratelimit.New(3, time.Hour), // 3 сообщения в час
		blocklist: ratelimit.NewBlocklist(),
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

	// Если сообщение от админа — обрабатываем команды
	if user.ID == h.adminID {
		h.handleAdminCommand(msg)
		return
	}

	// Проверка: заблокирован ли пользователь
	if h.blocklist.IsBlocked(user.ID) {
		h.stats.blockedCount++
		log.Printf("Заблокированный пользователь %d попытался отправить сообщение", user.ID)
		return // игнорируем, даже не отвечаем
	}

	// Проверка rate limit
	remaining := h.rl.Remaining(user.ID)
	if !h.rl.Allow(user.ID) {
		h.stats.blockedCount++
		log.Printf("Rate limit превышен для пользователя %d", user.ID)
		h.reply(msg.Chat.ID,
			fmt.Sprintf("Вы превысили лимит сообщений. Подождите, пожалуйста. Лимит: %d сообщ. в час.", h.rl.ResetInterval()/time.Hour),
		)
		return
	}

	// Пересылаем сообщение админу
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

	// Язык пользователя (если Telegram предоставил)
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
		h.reply(msg.Chat.ID, fmt.Sprintf(
			"📊 <b>Статистика бота</b>\n\n"+
				"Всего сообщений: %d\n"+
				"Переслано админу: %d\n"+
				"Заблокировано (спам): %d\n"+
				"Заблокировано (вручную): %d\n"+
				"Работает с: %s",
			h.stats.totalMessages,
			h.stats.forwardedCount,
			h.stats.blockedCount,
			len(h.blocklist.All()),
			h.stats.startedAt.Format("2006-01-02 15:04"),
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
		h.blocklist.Block(userID, reason)
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
		h.blocklist.Unblock(userID)
		h.reply(msg.Chat.ID, fmt.Sprintf("✅ Пользователь <code>%d</code> разблокирован.", userID))

	case "/blocklist":
		users := h.blocklist.All()
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
		// Если админ просто написал текст — не пересылаем
		h.reply(msg.Chat.ID, "Неизвестная команда. Используй /start для списка команд.")
	}
}

func parseInt64(s string) int64 {
	var id int64
	_, _ = fmt.Sscanf(s, "%d", &id)
	return id
}
