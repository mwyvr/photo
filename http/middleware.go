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

// --- CSRF / cross-origin protection -----------------------------------------

// crossOriginProtection wraps a handler with net/http's CrossOriginProtection
// (Go 1.25+), which blocks unsafe cross-origin browser requests by checking
// the Sec-Fetch-Site header (modern browsers) and falling back to comparing
// Origin against Host (older browsers that send Origin on POST).
//
// Requests with neither header — non-browser clients such as the photo CLI
// using Bearer token auth — are not browser-originated and are not subject
// to this check, by design of CrossOriginProtection.
//
// This is defense-in-depth alongside the SameSite=Lax session cookie: it
// protects state-changing API requests regardless of Content-Type, closing
// the gap where a cross-site form POST with enctype="text/plain" could
// otherwise be parsed as JSON by json.Decoder (which ignores both the
// declared Content-Type and trailing bytes after a valid JSON document).
func crossOriginProtection(next http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	return protection.Handler(next)
}

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
// request. trustedProxy is the IP of the reverse proxy; if non-empty,
// X-Forwarded-For is only trusted when the direct connection is from that IP.
func accessLog(next http.Handler, trustedProxy string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		ip := realIP(r, trustedProxy)
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

// realIP returns the client IP. X-Forwarded-For is only trusted when
// trustedProxy is empty (trust all) or matches the direct connection IP.
func realIP(r *http.Request, trustedProxy string) string {
	directIP, _, _ := net.SplitHostPort(r.RemoteAddr)

	// Only trust X-Forwarded-For from the configured proxy address.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if trustedProxy == "" || directIP == trustedProxy {
			if idx := strings.Index(xff, ","); idx != -1 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
	}
	if directIP != "" {
		return directIP
	}
	return r.RemoteAddr
}

// securityHeaders adds standard security response headers to every request.
// These mitigate clickjacking, MIME sniffing, XSS, and information leakage.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()

		// Prevent browsers from MIME-sniffing away from declared Content-Type.
		h.Set("X-Content-Type-Options", "nosniff")

		// Deny embedding in iframes — prevents clickjacking.
		h.Set("X-Frame-Options", "DENY")

		// Don't send the Referer header to external sites.
		h.Set("Referrer-Policy", "same-origin")

		// Content Security Policy:
		// - default-src 'self': only load resources from this origin
		// - img-src 'self' data:: allow inline data URIs for thumbnails
		// - style-src 'self' 'unsafe-inline': allow inline styles (used by onerror handlers)
		// - script-src 'self' 'unsafe-inline': required for the inline lightbox/nav JS
		// - frame-ancestors 'none': belt-and-suspenders against clickjacking
		// Note: 'unsafe-inline' for scripts is needed because the detail page has inline JS.
		// A future improvement would move JS to external files and use a nonce.
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"img-src 'self' data:; "+
				"style-src 'self' 'unsafe-inline'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"frame-ancestors 'none'",
		)

		// Remove the Server header to avoid leaking Go/version info.
		h.Set("Server", "photod")

		next.ServeHTTP(w, r)
	})
}



// rateLimiter tracks failed authentication attempts per IP address.
// After maxAttempts failures within window, further attempts are rejected
// with 429 Too Many Requests for the remainder of the window.
type rateLimiter struct {
	mu           sync.Mutex
	attempts     map[string]*ipAttempts
	max          int
	window       time.Duration
	trustedProxy string
}

type ipAttempts struct {
	count     int
	windowEnd time.Time
}

func newRateLimiter(max int, window time.Duration, trustedProxy string) *rateLimiter {
	rl := &rateLimiter{
		attempts:     make(map[string]*ipAttempts),
		max:          max,
		window:       window,
		trustedProxy: trustedProxy,
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
		ip := realIP(r, rl.trustedProxy)
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
