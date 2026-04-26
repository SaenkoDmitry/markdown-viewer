package limiter

import (
	"sync"
	"time"
)

type bucket struct {
	tokens   float64   // текущие токены
	lastLeak time.Time // когда последний раз вытекали
}

type RateLimiter struct {
	buckets  map[string]*bucket
	mu       sync.RWMutex
	rate     float64 // токенов в секунду (например, 0.5 = 30 в минуту)
	capacity float64 // максимум токенов (burst)
}

func NewRateLimiter(requestsPerMin int, burst int) *RateLimiter {
	return &RateLimiter{
		buckets:  make(map[string]*bucket),
		rate:     float64(requestsPerMin) / 60.0,
		capacity: float64(burst),
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.buckets[ip]

	if !exists {
		rl.buckets[ip] = &bucket{
			tokens:   rl.capacity - 1, // первый запрос
			lastLeak: now,
		}
		return true
	}

	// Вычисляем, сколько токенов вытекло с прошлого запроса
	elapsed := now.Sub(b.lastLeak).Seconds()
	leaked := elapsed * rl.rate

	b.tokens += leaked
	if b.tokens > rl.capacity {
		b.tokens = rl.capacity // не больше вместимости
	}

	b.lastLeak = now

	if b.tokens < 1 {
		return false // нет токенов
	}

	b.tokens--
	return true
}

// Cleanup удаляет пустые ведра
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, b := range rl.buckets {
			elapsed := now.Sub(b.lastLeak).Seconds()
			if elapsed > 60 && b.tokens >= rl.capacity {
				// Не использовался минуту и полный — удаляем
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}
