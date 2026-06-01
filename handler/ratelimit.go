package handler

import (
	"net/http"
	"sync"
	"time"
)

// ipLimiter 基于 IP 的简单令牌桶限流器
type ipLimiter struct {
	mu       sync.Mutex
	limiters map[string]*tokenBucket
	rate     int           // 每秒允许的请求数
	burst    int           // 突发容量
	cleanup  time.Duration // 清理间隔
}

type tokenBucket struct {
	tokens    float64
	lastTime  time.Time
	rate      float64
	burst     float64
}

// newIPLimiter 创建基于 IP 的限流器
func newIPLimiter(rate, burst int) *ipLimiter {
	limiter := &ipLimiter{
		limiters: make(map[string]*tokenBucket),
		rate:     rate,
		burst:    burst,
		cleanup:  5 * time.Minute,
	}

	// 定期清理不活跃的限流器
	go limiter.cleanupLoop()

	return limiter
}

func (l *ipLimiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanup)
	defer ticker.Stop()

	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for ip, bucket := range l.limiters {
			if now.Sub(bucket.lastTime) > l.cleanup {
				delete(l.limiters, ip)
			}
		}
		l.mu.Unlock()
	}
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, exists := l.limiters[ip]
	if !exists {
		bucket = &tokenBucket{
			tokens:   float64(l.burst),
			lastTime: time.Now(),
			rate:     float64(l.rate),
			burst:    float64(l.burst),
		}
		l.limiters[ip] = bucket
	}

	now := time.Now()
	elapsed := now.Sub(bucket.lastTime).Seconds()
	bucket.tokens += elapsed * bucket.rate
	if bucket.tokens > bucket.burst {
		bucket.tokens = bucket.burst
	}
	bucket.lastTime = now

	if bucket.tokens >= 1 {
		bucket.tokens--
		return true
	}
	return false
}

// RateLimitMiddleware 创建限流中间件
// rate: 每秒允许的请求数，burst: 突发容量
func RateLimitMiddleware(rate, burst int) func(http.HandlerFunc) http.HandlerFunc {
	limiter := newIPLimiter(rate, burst)

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			// 尝试从 X-Forwarded-For 获取真实 IP
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				ip = xff
			}

			if !limiter.allow(ip) {
				writeJSON(w, http.StatusTooManyRequests, 1, "请求过于频繁，请稍后再试", nil)
				return
			}
			next(w, r)
		}
	}
}
