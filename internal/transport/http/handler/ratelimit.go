package handler

import (
	"net/http"
	"sync"
	"time"

	"gofile/internal/infrastructure/cache/redis"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// ipLimiter 基于 IP 的令牌桶限流器（内存版，作为 Redis 不可用时的回退）
type ipLimiter struct {
	mu       sync.Mutex
	limiters map[string]*tokenBucket
	rate     int
	burst    int
	cleanup  time.Duration
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
	rate     float64
	burst    float64
}

// newIPLimiter 创建基于 IP 的限流器
func newIPLimiter(rate, burst int) *ipLimiter {
	limiter := &ipLimiter{
		limiters: make(map[string]*tokenBucket),
		rate:     rate,
		burst:    burst,
		cleanup:  5 * time.Minute,
	}

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

// redisRateLimiterScript Lua 固定窗口计数器脚本
var redisRateLimiterScript = redis.NewScript(`
	local key = KEYS[1]
	local now = tonumber(ARGV[1])
	local windowMs = tonumber(ARGV[2])
	local limit = tonumber(ARGV[3])
	local prefix = key .. ":" .. math.floor(now / windowMs)
	local current = redis.call("INCR", prefix)
	if current == 1 then
		redis.call("PEXPIRE", prefix, windowMs + 500)
	end
	return current <= limit
`)

// RateLimitMiddleware Gin 限流中间件
// 传入 cache 时用 Redis 全局限流；否则回退到内存限流（多实例下各自独立）
func RateLimitMiddleware(rate, burst int, c ...*cache.Client) gin.HandlerFunc {
	if len(c) > 0 && c[0] != nil {
		return newRedisRateLimiter(rate, burst, c[0])
	}
	limiter := newIPLimiter(rate, burst)
	return func(c *gin.Context) {
		if !limiter.allow(c.ClientIP()) {
			respondError(c, http.StatusTooManyRequests, CodeTooManyRequests, "请求过于频繁，请稍后再试")
			c.Abort()
			return
		}
		c.Next()
	}
}

// newRedisRateLimiter 基于 Redis 的限流器（Lua 原子固定窗口）
func newRedisRateLimiter(rate, burst int, c *cache.Client) gin.HandlerFunc {
	window := time.Second
	return func(ginCtx *gin.Context) {
		key := "gofile:ratelimit:" + ginCtx.ClientIP()
		nowMs := time.Now().UnixNano() / int64(time.Millisecond)

		result, err := redisRateLimiterScript.Run(ginCtx.Request.Context(), c.Rdb(),
			[]string{key}, nowMs, window.Milliseconds(), rate).Int()
		if err != nil || result == 0 {
			respondError(ginCtx, http.StatusTooManyRequests, CodeTooManyRequests, "请求过于频繁，请稍后再试")
			ginCtx.Abort()
			return
		}
		ginCtx.Next()
	}
}
