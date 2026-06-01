package ddos

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	zlog "github.com/rs/zerolog/log"
)

type ipInfo struct {
	requestCount int
	windowStart  time.Time
	blockedUntil time.Time
}

func New(enabled bool, maxRequestsPerSecond int, blockDuration time.Duration) func(http.Handler) http.Handler {
	if !enabled {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	zlog.Info().
		Int("maxRequestsPerSecond", maxRequestsPerSecond).
		Str("blockDuration", blockDuration.String()).
		Msg("[middleware/ddos] enabled")

	var mu sync.Mutex
	clients := make(map[string]*ipInfo)

	// Clean up old entries periodically to prevent memory leaks
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		for range ticker.C {
			mu.Lock()
			now := time.Now()
			for ip, info := range clients {
				if now.After(info.blockedUntil) && now.Sub(info.windowStart) > time.Minute {
					delete(clients, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract IP
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			// Handle proxies
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				ips := strings.Split(xff, ",")
				if len(ips) > 0 {
					ip = strings.TrimSpace(ips[0])
				}
			} else if xri := r.Header.Get("X-Real-IP"); xri != "" {
				ip = xri
			}

			now := time.Now()

			mu.Lock()
			info, exists := clients[ip]
			if !exists {
				info = &ipInfo{
					requestCount: 0,
					windowStart:  now,
				}
				clients[ip] = info
			}

			// Check if currently blocked
			if now.Before(info.blockedUntil) {
				mu.Unlock()
				zlog.Warn().Str("ip", ip).Msg("[middleware/ddos] blocked request intercepted")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"message": "too many requests, you are temporarily blocked",
						"code":    http.StatusTooManyRequests,
					},
				})
				return
			}

			// Reset window if more than 1 second has passed
			if now.Sub(info.windowStart) >= time.Second {
				info.requestCount = 0
				info.windowStart = now
			}

			info.requestCount++
			if info.requestCount > maxRequestsPerSecond {
				info.blockedUntil = now.Add(blockDuration)
				mu.Unlock()
				zlog.Warn().
					Str("ip", ip).
					Int("requestCount", info.requestCount).
					Msg("[middleware/ddos] IP blocked due to exceeding request rate limit")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"message": "too many requests, you are temporarily blocked",
						"code":    http.StatusTooManyRequests,
					},
				})
				return
			}

			mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}
