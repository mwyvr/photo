package http

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// --- Access logging ---------------------------------------------------------

// responseWriter wraps http.ResponseWriter to capture status code and bytes written.
type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	return n, err
}

// accessLog wraps a handler and writes one Apache Combined Log Format line per
// request. The real client IP is taken from X-Forwarded-For when present,
// which is correct when running behind a reverse proxy (Mox, Caddy, nginx).
//
// Format:
//
//	<ip> - - [<time>] "<method> <path> <proto>" <status> <bytes> "<referer>" "<user-agent>"
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		ip := realIP(r)
		ref := r.Referer()
		if ref == "" {
			ref = "-"
		}
		ua := r.UserAgent()
		if ua == "" {
			ua = "-"
		}

		log.Printf(`%s - - [%s] "%s %s %s" %d %d "%s" "%s" %.3fms`,
			ip,
			start.Format("02/Jan/2006:15:04:05 -0700"),
			r.Method, r.URL.RequestURI(), r.Proto,
			rw.status,
			rw.bytes,
			ref,
			ua,
			float64(time.Since(start).Microseconds())/1000.0,
		)
	})
}

// realIP returns the client IP from X-Forwarded-For (first entry) when set,
// otherwise falls back to RemoteAddr. Strips port from RemoteAddr.
func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For may be a comma-separated list; take the leftmost.
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// isBehindProxy returns true when the request arrived via a reverse proxy,
// indicated by the presence of X-Forwarded-Proto.
func isBehindProxy(r *http.Request) bool {
	return r.Header.Get("X-Forwarded-Proto") != ""
}

// isSecureRequest returns true when the original request used HTTPS,
// either directly (r.TLS != nil) or via a reverse proxy (X-Forwarded-Proto: https).
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// --- Rate limiting ----------------------------------------------------------

// rateLimiter tracks failed authentication attempts per IP address.
// After maxAttempts failures within window, further attempts are rejected
// with 429 Too Many Requests for the remainder of the window.
type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*ipAttempts
	max      int
	window   time.Duration
}

type ipAttempts struct {
	count     int
	windowEnd time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		attempts: make(map[string]*ipAttempts),
		max:      max,
		window:   window,
	}
	go rl.cleanup()
	return rl
}

// allow returns true if the IP is within its rate limit.
// Call record() after a failed attempt to increment the counter.
func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	a, ok := rl.attempts[ip]
	if !ok {
		return true
	}
	if time.Now().After(a.windowEnd) {
		delete(rl.attempts, ip)
		return true
	}
	return a.count < rl.max
}

// record increments the failure counter for an IP, starting a new window if needed.
func (rl *rateLimiter) record(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	a, ok := rl.attempts[ip]
	if !ok || now.After(a.windowEnd) {
		rl.attempts[ip] = &ipAttempts{count: 1, windowEnd: now.Add(rl.window)}
		return
	}
	a.count++
}

// cleanup removes expired entries every minute to prevent unbounded growth.
func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, a := range rl.attempts {
			if now.After(a.windowEnd) {
				delete(rl.attempts, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// rateLimit wraps a handler and enforces the rate limit.
// On a 401 response from the inner handler, the IP's failure count is incremented.
// On subsequent requests that exceed the limit, returns 429 immediately.
func (rl *rateLimiter) rateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := realIP(r)
		if !rl.allow(ip) {
			http.Error(w, fmt.Sprintf(`{"error":"too many failed attempts","code":"rate_limited"}`),
				http.StatusTooManyRequests)
			log.Printf("rate limit: blocked %s on %s %s", ip, r.Method, r.URL.Path)
			return
		}
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next(rw, r)
		if rw.status == http.StatusUnauthorized {
			rl.record(ip)
			log.Printf("rate limit: recorded failure for %s (%s %s)", ip, r.Method, r.URL.Path)
		}
	}
}
