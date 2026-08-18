package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultRequestIDHeader = "X-Request-ID"

	// DefaultContentSecurityPolicy permits scripts only from this server.
	DefaultContentSecurityPolicy = "default-src 'self'; base-uri 'self'; connect-src 'self'; font-src 'self' data:; form-action 'self'; frame-ancestors 'none'; frame-src 'none'; img-src 'self' data: blob:; manifest-src 'self'; media-src 'self'; object-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; worker-src 'self' blob:"
)

// ClientIPConfig controls when the server trusts proxy forwarding headers.
type ClientIPConfig struct {
	TrustedProxyCIDRs     []string
	TrustForwardedHeaders bool
}

// RateLimitPolicy defines a token-bucket rate and burst capacity.
type RateLimitPolicy struct {
	Requests int
	Window   time.Duration
	Burst    int
}

// RateLimitConfig controls IP and API-token rate limits.
type RateLimitConfig struct {
	AnonymousPerIP              RateLimitPolicy
	AuthenticatedPerToken       RateLimitPolicy
	CreatePerIdentity           RateLimitPolicy
	CreatePaths                 []string
	ApplyIPLimitToAuthenticated bool
	AuthenticatedIdentity       func(*http.Request) (string, bool)
	MaxIPEntries                int
	MaxTokenEntries             int
	EntryTTL                    time.Duration
	CleanupInterval             time.Duration
}

// RequestIDConfig controls request ID generation and propagation.
type RequestIDConfig struct {
	Enabled        bool
	Header         string
	AcceptIncoming bool
	Generator      func() string
}

// SecurityHeadersConfig controls browser security response headers.
type SecurityHeadersConfig struct {
	Enabled               bool
	ContentSecurityPolicy string
	PermissionsPolicy     string
	ReferrerPolicy        string
	StrictTransportPolicy string
}

// AccessLogConfig controls structured JSON access logging.
type AccessLogConfig struct {
	Enabled bool
	Writer  io.Writer
	Clock   func() time.Time
}

// HTTPMiddlewareConfig configures the complete HTTP middleware stack.
type HTTPMiddlewareConfig struct {
	ClientIP        ClientIPConfig
	RateLimit       RateLimitConfig
	RequestID       RequestIDConfig
	SecurityHeaders SecurityHeadersConfig
	AccessLog       AccessLogConfig
}

// DefaultHTTPMiddlewareConfig returns limits suitable for local development.
func DefaultHTTPMiddlewareConfig() HTTPMiddlewareConfig {
	return HTTPMiddlewareConfig{
		RateLimit: RateLimitConfig{
			AnonymousPerIP: RateLimitPolicy{
				Requests: 300,
				Window:   time.Minute,
				Burst:    60,
			},
			AuthenticatedPerToken: RateLimitPolicy{
				Requests: 1200,
				Window:   time.Minute,
				Burst:    120,
			},
			CreatePerIdentity: RateLimitPolicy{
				Requests: 60,
				Window:   time.Hour,
				Burst:    60,
			},
			CreatePaths: []string{
				"/api/pastes",
				"/api/saved_diffs",
				"/api/import",
			},
			MaxIPEntries:    4096,
			MaxTokenEntries: 4096,
			EntryTTL:        time.Hour,
			CleanupInterval: time.Minute,
		},
		RequestID: RequestIDConfig{
			Enabled:        true,
			Header:         defaultRequestIDHeader,
			AcceptIncoming: true,
		},
		SecurityHeaders: SecurityHeadersConfig{
			Enabled:               true,
			ContentSecurityPolicy: DefaultContentSecurityPolicy,
			PermissionsPolicy:     "camera=(), geolocation=(), microphone=(), payment=(), usb=()",
			ReferrerPolicy:        "no-referrer",
			StrictTransportPolicy: "max-age=31536000; includeSubDomains",
		},
		AccessLog: AccessLogConfig{
			Enabled: true,
			Writer:  os.Stdout,
		},
	}
}

// ClientIPResolver extracts a client address according to an explicit trust policy.
type ClientIPResolver struct {
	trusted               []netip.Prefix
	trustForwardedHeaders bool
}

// NewClientIPResolver validates and creates a client IP resolver.
func NewClientIPResolver(config ClientIPConfig) (*ClientIPResolver, error) {
	resolver := &ClientIPResolver{trustForwardedHeaders: config.TrustForwardedHeaders}
	for _, value := range config.TrustedProxyCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, errors.New("invalid trusted proxy CIDR: " + value)
		}
		resolver.trusted = append(resolver.trusted, prefix.Masked())
	}
	return resolver, nil
}

// ClientIP returns the resolved client IP without a port.
func (r *ClientIPResolver) ClientIP(request *http.Request) string {
	peer, ok := parseAddress(request.RemoteAddr)
	if !ok {
		return strings.TrimSpace(request.RemoteAddr)
	}
	if !r.trustForwardedHeaders || !r.isTrusted(peer) {
		return peer.String()
	}

	chains := [][]netip.Addr{
		parseForwardedHeader(request.Header.Values("Forwarded")),
		parseXForwardedFor(request.Header.Values("X-Forwarded-For")),
	}
	for _, chain := range chains {
		if len(chain) > 0 {
			return r.clientFromChain(chain).String()
		}
	}

	if realIP, valid := parseAddress(request.Header.Get("X-Real-IP")); valid {
		return realIP.String()
	}
	return peer.String()
}

func (r *ClientIPResolver) clientFromChain(chain []netip.Addr) netip.Addr {
	for index := len(chain) - 1; index >= 0; index-- {
		if !r.isTrusted(chain[index]) {
			return chain[index]
		}
	}
	return chain[0]
}

func (r *ClientIPResolver) isTrusted(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range r.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (r *ClientIPResolver) isTrustedRequest(request *http.Request) bool {
	peer, ok := parseAddress(request.RemoteAddr)
	return ok && r.trustForwardedHeaders && r.isTrusted(peer)
}

func parseAddress(value string) (netip.Addr, bool) {
	value = strings.Trim(strings.TrimSpace(value), `"`)
	if value == "" || strings.EqualFold(value, "unknown") || strings.HasPrefix(value, "_") {
		return netip.Addr{}, false
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.WithZone("").Unmap(), true
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		if address, parseErr := netip.ParseAddr(strings.Trim(host, "[]")); parseErr == nil {
			return address.WithZone("").Unmap(), true
		}
	}
	return netip.Addr{}, false
}

func parseForwardedHeader(values []string) []netip.Addr {
	var result []netip.Addr
	for _, value := range values {
		for _, element := range strings.Split(value, ",") {
			for _, parameter := range strings.Split(element, ";") {
				name, raw, found := strings.Cut(parameter, "=")
				if !found || !strings.EqualFold(strings.TrimSpace(name), "for") {
					continue
				}
				if address, ok := parseAddress(raw); ok {
					result = append(result, address)
				}
				break
			}
		}
	}
	return result
}

func parseXForwardedFor(values []string) []netip.Addr {
	var result []netip.Addr
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			if address, ok := parseAddress(raw); ok {
				result = append(result, address)
			}
		}
	}
	return result
}

type rateBucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
	events     []time.Time
}

type rateBucketStore struct {
	entries map[string]*rateBucket
	maximum int
}

// RateLimiter applies separate bounded token-bucket stores to IPs and tokens.
type RateLimiter struct {
	mu         sync.Mutex
	config     RateLimitConfig
	ipStore    rateBucketStore
	tokenStore rateBucketStore
	now        func() time.Time
	stop       chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
}

// NewRateLimiter validates the configuration and starts periodic cleanup.
func NewRateLimiter(config RateLimitConfig) (*RateLimiter, error) {
	normalized, err := normalizeRateLimitConfig(config)
	if err != nil {
		return nil, err
	}
	limiter := &RateLimiter{
		config: normalized,
		ipStore: rateBucketStore{
			entries: make(map[string]*rateBucket),
			maximum: normalized.MaxIPEntries,
		},
		tokenStore: rateBucketStore{
			entries: make(map[string]*rateBucket),
			maximum: normalized.MaxTokenEntries,
		},
		now:  time.Now,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go limiter.cleanupLoop()
	return limiter, nil
}

func normalizeRateLimitConfig(config RateLimitConfig) (RateLimitConfig, error) {
	if err := validateRateLimitPolicy(config.AnonymousPerIP); err != nil {
		return RateLimitConfig{}, errors.New("invalid anonymous per-IP rate limit: " + err.Error())
	}
	if err := validateRateLimitPolicy(config.AuthenticatedPerToken); err != nil {
		return RateLimitConfig{}, errors.New("invalid authenticated per-token rate limit: " + err.Error())
	}
	if err := validateRateLimitPolicy(config.CreatePerIdentity); err != nil {
		return RateLimitConfig{}, errors.New("invalid create rate limit: " + err.Error())
	}
	if config.MaxIPEntries < 0 || config.MaxTokenEntries < 0 {
		return RateLimitConfig{}, errors.New("rate limit entry bounds cannot be negative")
	}
	if config.MaxIPEntries == 0 {
		config.MaxIPEntries = 4096
	}
	if config.MaxTokenEntries == 0 {
		config.MaxTokenEntries = 4096
	}
	if config.AnonymousPerIP.Requests > 0 && config.AnonymousPerIP.Burst == 0 {
		config.AnonymousPerIP.Burst = config.AnonymousPerIP.Requests
	}
	if config.AuthenticatedPerToken.Requests > 0 && config.AuthenticatedPerToken.Burst == 0 {
		config.AuthenticatedPerToken.Burst = config.AuthenticatedPerToken.Requests
	}
	if config.CreatePerIdentity.Requests > 0 && config.CreatePerIdentity.Burst == 0 {
		config.CreatePerIdentity.Burst = config.CreatePerIdentity.Requests
	}
	if config.CreatePerIdentity.Requests > 0 && len(config.CreatePaths) == 0 {
		config.CreatePaths = []string{"/api/pastes", "/api/saved_diffs"}
	}
	if config.EntryTTL < 0 || config.CleanupInterval < 0 {
		return RateLimitConfig{}, errors.New("rate limit cleanup durations cannot be negative")
	}
	minimumTTL := maximumBucketRecoveryTime(config)
	if config.EntryTTL == 0 {
		config.EntryTTL = maxDuration(15*time.Minute, minimumTTL)
	} else if config.EntryTTL < minimumTTL {
		return RateLimitConfig{}, errors.New("rate limit entry TTL is shorter than a bucket recovery period")
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = time.Minute
	}
	return config, nil
}

func maximumBucketRecoveryTime(config RateLimitConfig) time.Duration {
	maximum := time.Duration(0)
	for _, policy := range []RateLimitPolicy{
		config.AnonymousPerIP,
		config.AuthenticatedPerToken,
		config.CreatePerIdentity,
	} {
		if policy.Requests == 0 {
			continue
		}
		recovery := time.Duration(math.Ceil(
			float64(policy.Window) * float64(policy.Burst) / float64(policy.Requests),
		))
		maximum = maxDuration(maximum, recovery)
	}
	return maximum
}

func validateRateLimitPolicy(policy RateLimitPolicy) error {
	if policy.Requests < 0 || policy.Burst < 0 {
		return errors.New("requests and burst cannot be negative")
	}
	if policy.Requests > 0 && policy.Window <= 0 {
		return errors.New("window must be greater than zero")
	}
	return nil
}

// Allow consumes general capacity for one anonymous IP or authenticated token.
func (l *RateLimiter) Allow(ipAddress, tokenIdentity string) (bool, time.Duration) {
	return l.allowRequest(ipAddress, tokenIdentity, false)
}

func (l *RateLimiter) allowRequest(ipAddress, tokenIdentity string, create bool) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	allowed := true
	var retryAfter time.Duration
	authenticated := tokenIdentity != ""
	if !authenticated || l.config.ApplyIPLimitToAuthenticated {
		accepted, retry := l.consume(&l.ipStore, "request:"+ipAddress, l.config.AnonymousPerIP, now)
		allowed = allowed && accepted
		retryAfter = maxDuration(retryAfter, retry)
	}
	if authenticated {
		digest := sha256.Sum256([]byte(tokenIdentity))
		key := hex.EncodeToString(digest[:])
		accepted, retry := l.consume(&l.tokenStore, "request:"+key, l.config.AuthenticatedPerToken, now)
		allowed = allowed && accepted
		retryAfter = maxDuration(retryAfter, retry)
	}
	if create && l.config.CreatePerIdentity.Requests > 0 {
		store := &l.ipStore
		key := "create:" + ipAddress
		if authenticated {
			store = &l.tokenStore
			digest := sha256.Sum256([]byte(tokenIdentity))
			key = "create:" + hex.EncodeToString(digest[:])
		}
		accepted, retry := l.consumeRollingWindow(store, key, l.config.CreatePerIdentity, now)
		allowed = allowed && accepted
		retryAfter = maxDuration(retryAfter, retry)
	}
	return allowed, retryAfter
}

func (l *RateLimiter) consumeRollingWindow(store *rateBucketStore, key string, policy RateLimitPolicy, now time.Time) (bool, time.Duration) {
	if policy.Requests == 0 {
		return true, 0
	}
	if key == "" {
		key = "unknown"
	}
	bucket, found := store.entries[key]
	if !found {
		if !l.ensureCapacity(store) {
			return false, maxDuration(l.config.CleanupInterval, time.Second)
		}
		bucket = &rateBucket{lastSeen: now}
		store.entries[key] = bucket
	}
	cutoff := now.Add(-policy.Window)
	firstActive := 0
	for firstActive < len(bucket.events) && !bucket.events[firstActive].After(cutoff) {
		firstActive++
	}
	if firstActive > 0 {
		copy(bucket.events, bucket.events[firstActive:])
		bucket.events = bucket.events[:len(bucket.events)-firstActive]
	}
	bucket.lastSeen = now
	capacity := policy.Requests
	if policy.Burst > 0 && policy.Burst < capacity {
		capacity = policy.Burst
	}
	if len(bucket.events) < capacity {
		bucket.events = append(bucket.events, now)
		return true, 0
	}
	retry := bucket.events[0].Add(policy.Window).Sub(now)
	return false, maxDuration(retry, time.Second)
}

func (l *RateLimiter) refundAnonymous(ipAddress string) {
	if l.config.AnonymousPerIP.Requests == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := "request:" + ipAddress
	bucket, found := l.ipStore.entries[key]
	if !found {
		return
	}
	bucket.tokens = math.Min(float64(l.config.AnonymousPerIP.Burst), bucket.tokens+1)
}

func (l *RateLimiter) consume(store *rateBucketStore, key string, policy RateLimitPolicy, now time.Time) (bool, time.Duration) {
	if policy.Requests == 0 {
		return true, 0
	}
	if key == "" {
		key = "unknown"
	}
	bucket, found := store.entries[key]
	if !found {
		if !l.ensureCapacity(store) {
			return false, maxDuration(l.config.CleanupInterval, time.Second)
		}
		bucket = &rateBucket{
			tokens:     float64(policy.Burst),
			lastRefill: now,
			lastSeen:   now,
		}
		store.entries[key] = bucket
	}

	elapsed := now.Sub(bucket.lastRefill)
	if elapsed > 0 {
		refillRate := float64(policy.Requests) / policy.Window.Seconds()
		bucket.tokens = math.Min(float64(policy.Burst), bucket.tokens+elapsed.Seconds()*refillRate)
		bucket.lastRefill = now
	}
	bucket.lastSeen = now
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true, 0
	}

	refillRate := float64(policy.Requests) / policy.Window.Seconds()
	retrySeconds := (1 - bucket.tokens) / refillRate
	retry := time.Duration(math.Ceil(retrySeconds * float64(time.Second)))
	return false, maxDuration(retry, time.Second)
}

func (l *RateLimiter) ensureCapacity(store *rateBucketStore) bool {
	return len(store.entries) < store.maximum
}

func (l *RateLimiter) cleanupStore(store *rateBucketStore, now time.Time) {
	cutoff := now.Add(-l.config.EntryTTL)
	for key, bucket := range store.entries {
		if !bucket.lastSeen.After(cutoff) {
			delete(store.entries, key)
		}
	}
}

// Cleanup removes expired rate limit entries immediately.
func (l *RateLimiter) Cleanup() {
	now := l.now()
	l.mu.Lock()
	l.cleanupStore(&l.ipStore, now)
	l.cleanupStore(&l.tokenStore, now)
	l.mu.Unlock()
}

// EntryCount reports the current IP and token entry counts.
func (l *RateLimiter) EntryCount() (int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.ipStore.entries), len(l.tokenStore.entries)
}

func (l *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(l.config.CleanupInterval)
	defer func() {
		ticker.Stop()
		close(l.done)
	}()
	for {
		select {
		case <-ticker.C:
			l.Cleanup()
		case <-l.stop:
			return
		}
	}
}

// Close stops the rate limiter cleanup worker.
func (l *RateLimiter) Close() {
	l.closeOnce.Do(func() {
		close(l.stop)
		<-l.done
	})
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

type provisionalAnonymousRateContextKey struct{}

type provisionalAnonymousRate struct {
	ipAddress  string
	allowed    bool
	classified atomic.Bool
}

type requestIDContextKey struct{}

var fallbackRequestIDCounter atomic.Uint64

// RequestIDFromContext returns the request ID assigned by the middleware.
func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

func generateRequestID() string {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err == nil {
		return base64.RawURLEncoding.EncodeToString(randomBytes)
	}
	sequence := fallbackRequestIDCounter.Add(1)
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatUint(sequence, 36)
}

func validRequestID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("._:-", char) {
			continue
		}
		return false
	}
	return true
}

// NewRequestIDMiddleware returns middleware that assigns validated request IDs.
func NewRequestIDMiddleware(config RequestIDConfig) func(http.Handler) http.Handler {
	header := config.Header
	if header == "" {
		header = defaultRequestIDHeader
	}
	generator := config.Generator
	if generator == nil {
		generator = generateRequestID
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if !config.Enabled {
				next.ServeHTTP(writer, request)
				return
			}
			requestID := ""
			if config.AcceptIncoming {
				candidate := request.Header.Get(header)
				if validRequestID(candidate) {
					requestID = candidate
				}
			}
			if requestID == "" {
				requestID = generator()
				if !validRequestID(requestID) {
					requestID = generateRequestID()
				}
			}
			writer.Header().Set(header, requestID)
			ctx := context.WithValue(request.Context(), requestIDContextKey{}, requestID)
			next.ServeHTTP(writer, request.WithContext(ctx))
		})
	}
}

// NewSecurityPolicyMiddleware returns strict browser security middleware.
func NewSecurityPolicyMiddleware(config SecurityHeadersConfig, resolver *ClientIPResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if !config.Enabled {
				next.ServeHTTP(writer, request)
				return
			}
			headers := writer.Header()
			if config.ContentSecurityPolicy != "" {
				headers.Set("Content-Security-Policy", config.ContentSecurityPolicy)
			}
			headers.Set("Cross-Origin-Opener-Policy", "same-origin")
			headers.Set("Cross-Origin-Resource-Policy", "same-origin")
			headers.Set("Origin-Agent-Cluster", "?1")
			if config.PermissionsPolicy != "" {
				headers.Set("Permissions-Policy", config.PermissionsPolicy)
			}
			if config.ReferrerPolicy != "" {
				headers.Set("Referrer-Policy", config.ReferrerPolicy)
			}
			headers.Set("X-Content-Type-Options", "nosniff")
			headers.Set("X-DNS-Prefetch-Control", "off")
			headers.Set("X-Frame-Options", "DENY")
			headers.Set("X-Permitted-Cross-Domain-Policies", "none")
			if config.StrictTransportPolicy != "" && requestIsHTTPS(request, resolver) {
				headers.Set("Strict-Transport-Security", config.StrictTransportPolicy)
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func requestIsHTTPS(request *http.Request, resolver *ClientIPResolver) bool {
	if request.TLS != nil {
		return true
	}
	if resolver == nil || !resolver.isTrustedRequest(request) {
		return false
	}
	forwardedProto := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(forwardedProto, "https")
}

// NewRateLimitMiddleware returns middleware for anonymous and authenticated limits.
// Authentication middleware must run first to provide a verified principal.
func NewRateLimitMiddleware(limiter *RateLimiter, resolver *ClientIPResolver, config RateLimitConfig) func(http.Handler) http.Handler {
	if limiter != nil {
		config = limiter.config
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			ipAddress := strings.TrimSpace(request.RemoteAddr)
			if resolver != nil {
				ipAddress = resolver.ClientIP(request)
			}
			identity := ""
			if config.AuthenticatedIdentity != nil {
				if authenticatedIdentity, ok := config.AuthenticatedIdentity(request); ok {
					identity = authenticatedIdentity
				}
			}
			reservation, reserved := request.Context().Value(provisionalAnonymousRateContextKey{}).(*provisionalAnonymousRate)
			if reserved && identity != "" {
				if reservation.allowed {
					limiter.refundAnonymous(reservation.ipAddress)
				}
				reservation.classified.Store(true)
			}
			allowed := true
			var retryAfter time.Duration
			if !reserved || identity != "" {
				allowed, retryAfter = limiter.allowRequest(ipAddress, identity, isCreateRequest(request, config.CreatePaths))
			} else if !reservation.allowed {
				allowed = false
				retryAfter = time.Second
			}
			if !allowed {
				writeRateLimitResponse(writer, retryAfter)
				return
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func writeRateLimitResponse(writer http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Retry-After", strconv.Itoa(seconds))
	writer.WriteHeader(http.StatusTooManyRequests)
	_, _ = io.WriteString(writer, "{\"error\":\"rate limit exceeded\"}\n")
}

func (middleware *HTTPMiddleware) reserveAnonymousForBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization := request.Header.Get("Authorization")
		if len(authorization) > maxBearerTokenLength+len("Bearer ") {
			ipAddress := strings.TrimSpace(request.RemoteAddr)
			if middleware.resolver != nil {
				ipAddress = middleware.resolver.ClientIP(request)
			}
			allowed, retryAfter := middleware.limiter.Allow(ipAddress, "")
			if !allowed {
				writeRateLimitResponse(writer, retryAfter)
				return
			}
			respondJSON(writer, http.StatusUnauthorized, map[string]string{"error": "Invalid API token"})
			return
		}
		if !bearerCredentialPresent(authorization) {
			next.ServeHTTP(writer, request)
			return
		}
		ipAddress := strings.TrimSpace(request.RemoteAddr)
		if middleware.resolver != nil {
			ipAddress = middleware.resolver.ClientIP(request)
		}
		allowed, retryAfter := middleware.limiter.Allow(ipAddress, "")
		reservation := &provisionalAnonymousRate{ipAddress: ipAddress, allowed: allowed}
		ctx := context.WithValue(
			request.Context(),
			provisionalAnonymousRateContextKey{},
			reservation,
		)
		if allowed {
			next.ServeHTTP(writer, request.WithContext(ctx))
			return
		}
		response := &provisionalRateResponseWriter{
			ResponseWriter: writer,
			reservation:    reservation,
			retryAfter:     retryAfter,
		}
		next.ServeHTTP(response, request.WithContext(ctx))
		if !response.wroteHeader {
			response.WriteHeader(http.StatusOK)
		}
	})
}

type provisionalRateResponseWriter struct {
	http.ResponseWriter
	reservation *provisionalAnonymousRate
	retryAfter  time.Duration
	wroteHeader bool
	rejected    bool
}

func (writer *provisionalRateResponseWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}
	writer.wroteHeader = true
	if !writer.reservation.classified.Load() {
		writer.rejected = true
		writeRateLimitResponse(writer.ResponseWriter, writer.retryAfter)
		return
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *provisionalRateResponseWriter) Write(value []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
	if writer.rejected {
		return len(value), nil
	}
	return writer.ResponseWriter.Write(value)
}

func (writer *provisionalRateResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func bearerCredentialPresent(header string) bool {
	if len(header) > maxBearerTokenLength+len("Bearer ") {
		return false
	}
	parts := strings.Fields(header)
	return len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != ""
}

func isCreateRequest(request *http.Request, paths []string) bool {
	if request.Method != http.MethodPost {
		return false
	}
	for _, path := range paths {
		if request.URL.Path == path {
			return true
		}
	}
	return false
}

type accessResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (writer *accessResponseWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *accessResponseWriter) Write(value []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	written, err := writer.ResponseWriter.Write(value)
	writer.bytes += written
	return written, err
}

func (writer *accessResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

type jsonAccessLogger struct {
	mu     sync.Mutex
	writer io.Writer
	clock  func() time.Time
}

type accessLogRecord struct {
	Timestamp  string  `json:"timestamp"`
	RequestID  string  `json:"request_id"`
	RemoteIP   string  `json:"remote_ip"`
	Method     string  `json:"method"`
	Path       string  `json:"path"`
	Status     int     `json:"status"`
	Bytes      int     `json:"bytes"`
	DurationMS float64 `json:"duration_ms"`
	Panicked   bool    `json:"panicked,omitempty"`
}

// NewJSONAccessLogMiddleware returns content-free structured access logging.
func NewJSONAccessLogMiddleware(config AccessLogConfig, resolver *ClientIPResolver) func(http.Handler) http.Handler {
	output := config.Writer
	if output == nil {
		output = os.Stdout
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	logger := &jsonAccessLogger{writer: output, clock: clock}
	return logger.middleware(config.Enabled, resolver)
}

func (logger *jsonAccessLogger) middleware(enabled bool, resolver *ClientIPResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if !enabled {
				next.ServeHTTP(writer, request)
				return
			}
			startedAt := logger.clock()
			recorder := &accessResponseWriter{ResponseWriter: writer}
			panicked := true
			defer func() {
				status := recorder.status
				if status == 0 {
					status = http.StatusOK
				}
				if panicked && status < http.StatusInternalServerError {
					status = http.StatusInternalServerError
				}
				remoteIP := strings.TrimSpace(request.RemoteAddr)
				if resolver != nil {
					remoteIP = resolver.ClientIP(request)
				}
				record := accessLogRecord{
					Timestamp:  startedAt.UTC().Format(time.RFC3339Nano),
					RequestID:  RequestIDFromContext(request.Context()),
					RemoteIP:   remoteIP,
					Method:     request.Method,
					Path:       request.URL.Path,
					Status:     status,
					Bytes:      recorder.bytes,
					DurationMS: float64(logger.clock().Sub(startedAt).Microseconds()) / 1000,
					Panicked:   panicked,
				}
				logger.write(record)
			}()
			next.ServeHTTP(recorder, request)
			panicked = false
		})
	}
}

func (logger *jsonAccessLogger) write(record accessLogRecord) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	_ = json.NewEncoder(logger.writer).Encode(record)
}

// HTTPMiddleware owns the shared state for the complete middleware stack.
type HTTPMiddleware struct {
	config   HTTPMiddlewareConfig
	resolver *ClientIPResolver
	limiter  *RateLimiter
}

// NewHTTPMiddleware validates and creates the complete middleware stack.
func NewHTTPMiddleware(config HTTPMiddlewareConfig) (*HTTPMiddleware, error) {
	resolver, err := NewClientIPResolver(config.ClientIP)
	if err != nil {
		return nil, err
	}
	limiter, err := NewRateLimiter(config.RateLimit)
	if err != nil {
		return nil, err
	}
	config.RateLimit = limiter.config
	return &HTTPMiddleware{config: config, resolver: resolver, limiter: limiter}, nil
}

// Wrap composes request IDs, JSON logs, security headers, and rate limits.
// An outer authentication middleware can provide a verified principal.
func (middleware *HTTPMiddleware) Wrap(next http.Handler) http.Handler {
	handler := NewRateLimitMiddleware(
		middleware.limiter,
		middleware.resolver,
		middleware.config.RateLimit,
	)(next)
	handler = NewSecurityPolicyMiddleware(middleware.config.SecurityHeaders, middleware.resolver)(handler)
	handler = NewJSONAccessLogMiddleware(middleware.config.AccessLog, middleware.resolver)(handler)
	handler = NewRequestIDMiddleware(middleware.config.RequestID)(handler)
	return handler
}

// WrapWithAuthentication runs authentication before rate classification.
func (middleware *HTTPMiddleware) WrapWithAuthentication(next http.Handler, authentication func(http.Handler) http.Handler) http.Handler {
	handler := NewRateLimitMiddleware(
		middleware.limiter,
		middleware.resolver,
		middleware.config.RateLimit,
	)(next)
	if authentication != nil {
		handler = authentication(handler)
		handler = middleware.reserveAnonymousForBearer(handler)
	}
	handler = NewSecurityPolicyMiddleware(middleware.config.SecurityHeaders, middleware.resolver)(handler)
	handler = NewJSONAccessLogMiddleware(middleware.config.AccessLog, middleware.resolver)(handler)
	handler = NewRequestIDMiddleware(middleware.config.RequestID)(handler)
	return handler
}

// Close stops background middleware workers.
func (middleware *HTTPMiddleware) Close() {
	if middleware != nil && middleware.limiter != nil {
		middleware.limiter.Close()
	}
}
