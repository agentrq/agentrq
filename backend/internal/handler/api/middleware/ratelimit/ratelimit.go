package ratelimit

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	zlog "github.com/rs/zerolog/log"
)

type clientInfo struct {
	requestCount int
	windowStart  time.Time
}

func New(enabled bool, maxPerIP int, maxPerUser int, window time.Duration, tokenSvc auth.TokenService) func(http.Handler) http.Handler {
	if !enabled {
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	zlog.Info().
		Int("maxPerIP", maxPerIP).
		Int("maxPerUser", maxPerUser).
		Str("window", window.String()).
		Msg("[middleware/ratelimit] enabled")

	var mu sync.Mutex
	clients := make(map[string]*clientInfo)

	// Clean up old entries
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		for range ticker.C {
			mu.Lock()
			now := time.Now()
			for key, info := range clients {
				if now.Sub(info.windowStart) > window*2 {
					delete(clients, key)
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Always extract IP
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				ips := strings.Split(xff, ",")
				if len(ips) > 0 {
					ip = strings.TrimSpace(ips[0])
				}
			} else if xri := r.Header.Get("X-Real-IP"); xri != "" {
				ip = xri
			}
			ipKey := "ip:" + ip

			// Extract User ID if authenticated
			userKey := ""

			// 1. Try resolving authenticated User ID from JWT cookie
			if cookie, err := r.Cookie("at"); err == nil && cookie.Value != "" {
				if claims, err := tokenSvc.ValidateToken(cookie.Value); err == nil && claims.Subject != "" {
					userKey = "user:" + claims.Subject
				}
			}

			// 2. Try resolving from Authorization header
			if userKey == "" {
				if authHeader := r.Header.Get("Authorization"); authHeader != "" {
					tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
					tokenStr = strings.TrimSpace(tokenStr)
					if claims, err := tokenSvc.ValidateToken(tokenStr); err == nil && claims.Subject != "" {
						userKey = "user:" + claims.Subject
					}
				}
			}

			// 3. Try resolving from query parameters
			if userKey == "" {
				if tokenStr := r.URL.Query().Get("token"); tokenStr != "" {
					if claims, err := tokenSvc.ValidateToken(tokenStr); err == nil && claims.Subject != "" {
						userKey = "user:" + claims.Subject
					}
				}
			}

			now := time.Now()

			mu.Lock()

			// Enforce IP-based rate limit first
			ipInfo, ipExists := clients[ipKey]
			if !ipExists {
				ipInfo = &clientInfo{
					requestCount: 0,
					windowStart:  now,
				}
				clients[ipKey] = ipInfo
			}

			if now.Sub(ipInfo.windowStart) >= window {
				ipInfo.requestCount = 0
				ipInfo.windowStart = now
			}

			ipInfo.requestCount++
			if ipInfo.requestCount > maxPerIP {
				mu.Unlock()
				zlog.Warn().Str("key", ipKey).Msg("[middleware/ratelimit] IP ratelimit reached")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"message": "ip ratelimited",
						"code":    http.StatusTooManyRequests,
					},
				})
				return
			}

			// Enforce User-based rate limit next (if authenticated)
			var userInfo *clientInfo
			if userKey != "" {
				var userExists bool
				userInfo, userExists = clients[userKey]
				if !userExists {
					userInfo = &clientInfo{
						requestCount: 0,
						windowStart:  now,
					}
					clients[userKey] = userInfo
				}

				if now.Sub(userInfo.windowStart) >= window {
					userInfo.requestCount = 0
					userInfo.windowStart = now
				}

				userInfo.requestCount++
				if userInfo.requestCount > maxPerUser {
					// Revert IP count so blocked user requests don't waste the IP limit quota
					ipInfo.requestCount--
					mu.Unlock()
					zlog.Warn().Str("key", userKey).Msg("[middleware/ratelimit] User ratelimit reached")
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"error": map[string]any{
							"message": "user ratelimited",
							"code":    http.StatusTooManyRequests,
						},
					})
					return
				}
			}

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit-IP", strconv.Itoa(maxPerIP))
			w.Header().Set("X-RateLimit-Remaining-IP", strconv.Itoa(maxPerIP - ipInfo.requestCount))
			w.Header().Set("X-RateLimit-Reset-IP", strconv.FormatInt(ipInfo.windowStart.Add(window).Unix(), 10))

			if userKey != "" {
				w.Header().Set("X-RateLimit-Limit-User", strconv.Itoa(maxPerUser))
				w.Header().Set("X-RateLimit-Remaining-User", strconv.Itoa(maxPerUser - userInfo.requestCount))
				w.Header().Set("X-RateLimit-Reset-User", strconv.FormatInt(userInfo.windowStart.Add(window).Unix(), 10))

				// Set primary headers to User limits since it's the more specific limit
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(maxPerUser))
				w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(maxPerUser - userInfo.requestCount))
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(userInfo.windowStart.Add(window).Unix(), 10))
			} else {
				// Set primary headers to IP limits
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(maxPerIP))
				w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(maxPerIP - ipInfo.requestCount))
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(ipInfo.windowStart.Add(window).Unix(), 10))
			}

			mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}
