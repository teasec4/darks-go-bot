package ratelimit

import "sync"

// Blocklist — список заблокированных пользователей.
// Хранится в памяти, при перезапуске бота сбрасывается.
type Blocklist struct {
	mu   sync.RWMutex
	set  map[int64]string // userID -> причина (пустая строка если без причины)
}

func NewBlocklist() *Blocklist {
	return &Blocklist{set: make(map[int64]string)}
}

func (b *Blocklist) Block(userID int64, reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.set[userID] = reason
}

func (b *Blocklist) Unblock(userID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.set, userID)
}

func (b *Blocklist) IsBlocked(userID int64) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.set[userID]
	return ok
}

func (b *Blocklist) All() map[int64]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cp := make(map[int64]string, len(b.set))
	for k, v := range b.set {
		cp[k] = v
	}
	return cp
}
