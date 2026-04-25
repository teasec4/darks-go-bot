package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// FeedbackRecord — запись обратной связи.
type FeedbackRecord struct {
	ID          int64
	UserID      int64
	UserName    string
	FirstName   string
	MessageText string
	Forwarded   bool
	CreatedAt   time.Time
}

// BotStats — сводная статистика.
type BotStats struct {
	TotalFeedbacks int
	Forwarded      int
	BlockedUsers   int
	Since          time.Time
}

// Storage — работа с БД.
type Storage struct {
	db *sql.DB
}

func New(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Настройки
	db.SetMaxOpenConns(1) // SQLite не любит конкурентные записи

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	s := &Storage{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func (s *Storage) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS blocked_users (
			user_id   INTEGER PRIMARY KEY,
			reason    TEXT NOT NULL DEFAULT '',
			blocked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS feedback (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id     INTEGER NOT NULL,
			user_name   TEXT DEFAULT '',
			first_name  TEXT DEFAULT '',
			message_text TEXT NOT NULL,
			forwarded   INTEGER DEFAULT 0,
			created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// --- Blocklist ---

func (s *Storage) BlockUser(userID int64, reason string) error {
	_, err := s.db.Exec(
		`INSERT INTO blocked_users (user_id, reason) VALUES (?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET reason = excluded.reason`,
		userID, reason,
	)
	return err
}

func (s *Storage) UnblockUser(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM blocked_users WHERE user_id = ?`, userID)
	return err
}

func (s *Storage) IsBlocked(userID int64) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM blocked_users WHERE user_id = ?`, userID).Scan(&count)
	return count > 0, err
}

func (s *Storage) GetAllBlocked() (map[int64]string, error) {
	rows, err := s.db.Query(`SELECT user_id, reason FROM blocked_users ORDER BY blocked_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]string)
	for rows.Next() {
		var id int64
		var reason string
		if err := rows.Scan(&id, &reason); err != nil {
			return nil, err
		}
		result[id] = reason
	}
	return result, rows.Err()
}

// --- Feedback ---

func (s *Storage) SaveFeedback(userID int64, userName, firstName, text string) error {
	_, err := s.db.Exec(
		`INSERT INTO feedback (user_id, user_name, first_name, message_text) VALUES (?, ?, ?, ?)`,
		userID, userName, firstName, text,
	)
	return err
}

func (s *Storage) MarkForwarded(id int64) error {
	_, err := s.db.Exec(`UPDATE feedback SET forwarded = 1 WHERE id = ?`, id)
	return err
}

// --- Stats ---

func (s *Storage) GetStats() (*BotStats, error) {
	stats := &BotStats{}

	err := s.db.QueryRow(`SELECT COUNT(*) FROM feedback`).Scan(&stats.TotalFeedbacks)
	if err != nil {
		return nil, err
	}

	err = s.db.QueryRow(`SELECT COUNT(*) FROM feedback WHERE forwarded = 1`).Scan(&stats.Forwarded)
	if err != nil {
		return nil, err
	}

	err = s.db.QueryRow(`SELECT COUNT(*) FROM blocked_users`).Scan(&stats.BlockedUsers)
	if err != nil {
		return nil, err
	}

	// SQLite хранит TIMESTAMP как строку, сканируем в string и парсим
	var sinceStr string
	err = s.db.QueryRow(`SELECT COALESCE(MIN(created_at), datetime('now', 'localtime')) FROM feedback`).Scan(&sinceStr)
	if err != nil {
		return nil, err
	}
	stats.Since, err = time.Parse("2006-01-02 15:04:05", sinceStr)
	if err != nil {
		// fallback — на текущее время
		stats.Since = time.Now()
	}

	return stats, nil
}
