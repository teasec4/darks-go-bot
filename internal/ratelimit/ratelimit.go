package ratelimit

import (
	"sync"
	"time"
)

type entry struct {
	count    int
	windowAt time.Time
}

type RateLimiter struct {
	mu       sync.RWMutex
	users    map[int64]*entry
	limit    int
	interval time.Duration
}

func New(limit int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		users:    make(map[int64]*entry),
		limit:    limit,
		interval: interval,
	}
}

// Allow проверяет, может ли пользователь отправить сообщение.
// Возвращает true, если лимит не превышен.
func (rl *RateLimiter) Allow(userID int64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	e, ok := rl.users[userID]
	now := time.Now()

	if !ok || now.Sub(e.windowAt) > rl.interval {
		// Новое окно
		rl.users[userID] = &entry{count: 1, windowAt: now}
		return true
	}

	if e.count >= rl.limit {
		return false
	}

	e.count++
	return true
}

// Remaining возвращает сколько ещё сообщений можно отправить в текущем окне.
func (rl *RateLimiter) Remaining(userID int64) int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	e, ok := rl.users[userID]
	if !ok {
		return rl.limit
	}
	if time.Since(e.windowAt) > rl.interval {
		return rl.limit
	}
	rem := rl.limit - e.count
	if rem < 0 {
		rem = 0
	}
	return rem
}

// ResetInterval возвращает длительность окна.
func (rl *RateLimiter) ResetInterval() time.Duration {
	return rl.interval
}

// cleanup удаляет записи, у которых окно истекло.
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for id, e := range rl.users {
		if now.Sub(e.windowAt) > rl.interval {
			delete(rl.users, id)
		}
	}
}

// StartCleanup запускает фоновую горутину для очистки старых записей.
// Останавливается через переданный канал.
func (rl *RateLimiter) StartCleanup(interval time.Duration, stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rl.cleanup()
			case <-stop:
				return
			}
		}
	}()
}
