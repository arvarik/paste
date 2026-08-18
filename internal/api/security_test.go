package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimiterAppliesIPAndTokenPolicies(t *testing.T) {
	t.Run("per IP", func(t *testing.T) {
		limiter, err := NewRateLimiter(RateLimitConfig{
			AnonymousPerIP: RateLimitPolicy{Requests: 2, Window: time.Hour, Burst: 2},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer limiter.Close()

		for attempt := 0; attempt < 2; attempt++ {
			if allowed, _ := limiter.Allow("192.0.2.10", ""); !allowed {
				t.Fatalf("attempt %d was rejected", attempt+1)
			}
		}
		allowed, retryAfter := limiter.Allow("192.0.2.10", "")
		if allowed {
			t.Fatal("third request was accepted")
		}
		if retryAfter < time.Second {
			t.Fatalf("retry delay = %v, want at least one second", retryAfter)
		}
		if allowed, _ := limiter.Allow("192.0.2.11", ""); !allowed {
			t.Fatal("a different IP was rejected")
		}
	})

	t.Run("per token across IPs", func(t *testing.T) {
		limiter, err := NewRateLimiter(RateLimitConfig{
			AuthenticatedPerToken: RateLimitPolicy{Requests: 1, Window: time.Hour, Burst: 1},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer limiter.Close()

		if allowed, _ := limiter.Allow("192.0.2.20", "shared-token"); !allowed {
			t.Fatal("first token request was rejected")
		}
		if allowed, _ := limiter.Allow("192.0.2.21", "shared-token"); allowed {
			t.Fatal("the same token bypassed its limit from a second IP")
		}
		if allowed, _ := limiter.Allow("192.0.2.21", "other-token"); !allowed {
			t.Fatal("a different token was rejected")
		}
	})
}

func TestAuthenticatedRequestsDoNotConsumeAnonymousLimitByDefault(t *testing.T) {
	limiter, err := NewRateLimiter(RateLimitConfig{
		AnonymousPerIP:        RateLimitPolicy{Requests: 1, Window: time.Hour, Burst: 1},
		AuthenticatedPerToken: RateLimitPolicy{Requests: 3, Window: time.Hour, Burst: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer limiter.Close()

	if accepted, _ := limiter.Allow("192.0.2.25", ""); !accepted {
		t.Fatal("first anonymous request was rejected")
	}
	if accepted, _ := limiter.Allow("192.0.2.25", ""); accepted {
		t.Fatal("second anonymous request bypassed its IP limit")
	}
	for attempt := 0; attempt < 3; attempt++ {
		if accepted, _ := limiter.Allow("192.0.2.25", "verified-token-id"); !accepted {
			t.Fatalf("authenticated attempt %d consumed the anonymous limit", attempt+1)
		}
	}
	if accepted, _ := limiter.Allow("192.0.2.25", "verified-token-id"); accepted {
		t.Fatal("authenticated request bypassed its token limit")
	}
}

func TestCreatePerHourLimitUsesAuthenticatedOrAnonymousIdentity(t *testing.T) {
	limiter, err := NewRateLimiter(RateLimitConfig{
		AnonymousPerIP:        RateLimitPolicy{Requests: 100, Window: time.Minute, Burst: 100},
		AuthenticatedPerToken: RateLimitPolicy{Requests: 100, Window: time.Minute, Burst: 100},
		CreatePerIdentity:     RateLimitPolicy{Requests: 2, Window: time.Hour, Burst: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer limiter.Close()

	for attempt := 0; attempt < 2; attempt++ {
		if accepted, _ := limiter.allowRequest("192.0.2.26", "", true); !accepted {
			t.Fatalf("anonymous create %d was rejected", attempt+1)
		}
	}
	if accepted, _ := limiter.allowRequest("192.0.2.26", "", true); accepted {
		t.Fatal("anonymous create bypassed the hourly limit")
	}

	for attempt := 0; attempt < 2; attempt++ {
		if accepted, _ := limiter.allowRequest("192.0.2.26", "verified-token-id", true); !accepted {
			t.Fatalf("authenticated create %d inherited the anonymous create limit", attempt+1)
		}
	}
	if accepted, retryAfter := limiter.allowRequest("198.51.100.26", "verified-token-id", true); accepted || retryAfter < time.Second {
		t.Fatalf("authenticated create result = %v, %v", accepted, retryAfter)
	}
}

func TestCreateLimitUsesARollingWindow(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	limiter, err := NewRateLimiter(RateLimitConfig{
		AnonymousPerIP:    RateLimitPolicy{Requests: 100, Window: time.Minute, Burst: 100},
		CreatePerIdentity: RateLimitPolicy{Requests: 2, Window: time.Hour, Burst: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer limiter.Close()
	limiter.now = func() time.Time { return now }

	if allowed, _ := limiter.allowRequest("192.0.2.27", "", true); !allowed {
		t.Fatal("first create was rejected")
	}
	now = now.Add(59 * time.Minute)
	if allowed, _ := limiter.allowRequest("192.0.2.27", "", true); !allowed {
		t.Fatal("second create was rejected")
	}
	if allowed, _ := limiter.allowRequest("192.0.2.27", "", true); allowed {
		t.Fatal("third create bypassed the rolling limit")
	}
	now = now.Add(2 * time.Minute)
	if allowed, _ := limiter.allowRequest("192.0.2.27", "", true); !allowed {
		t.Fatal("capacity did not expire after the first event left the window")
	}
	if allowed, _ := limiter.allowRequest("192.0.2.27", "", true); allowed {
		t.Fatal("the boundary reset admitted a burst above the rolling limit")
	}
}

func TestDefaultCreatePathsIncludeImport(t *testing.T) {
	paths := DefaultHTTPMiddlewareConfig().RateLimit.CreatePaths
	for _, path := range paths {
		if path == "/api/import" {
			return
		}
	}
	t.Fatalf("default create paths omit /api/import: %v", paths)
}

func TestRateLimiterRejectsCleanupBeforeBucketRecovery(t *testing.T) {
	_, err := NewRateLimiter(RateLimitConfig{
		CreatePerIdentity: RateLimitPolicy{Requests: 1, Window: time.Hour, Burst: 1},
		EntryTTL:          15 * time.Minute,
	})
	if err == nil {
		t.Fatal("an entry TTL shorter than the hourly bucket recovery was accepted")
	}
}

func TestRateLimitMiddlewareReturnsOperationalHeaders(t *testing.T) {
	rateConfig := RateLimitConfig{
		AnonymousPerIP: RateLimitPolicy{Requests: 1, Window: time.Hour, Burst: 1},
	}
	limiter, err := NewRateLimiter(rateConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer limiter.Close()
	resolver, err := NewClientIPResolver(ClientIPConfig{})
	if err != nil {
		t.Fatal(err)
	}

	handler := NewRateLimitMiddleware(limiter, resolver, rateConfig)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))

	first := httptest.NewRequest(http.MethodGet, "/", nil)
	first.RemoteAddr = "192.0.2.30:3000"
	handler.ServeHTTP(httptest.NewRecorder(), first)

	second := httptest.NewRequest(http.MethodGet, "/", nil)
	second.RemoteAddr = "192.0.2.30:3001"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, second)

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
	if response.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header is missing")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestRateLimitMiddlewareAppliesConfiguredCreatePaths(t *testing.T) {
	rateConfig := RateLimitConfig{
		AnonymousPerIP:        RateLimitPolicy{Requests: 100, Window: time.Minute, Burst: 100},
		AuthenticatedPerToken: RateLimitPolicy{Requests: 100, Window: time.Minute, Burst: 100},
		CreatePerIdentity:     RateLimitPolicy{Requests: 1, Window: time.Hour, Burst: 1},
		CreatePaths:           []string{"/api/pastes"},
		AuthenticatedIdentity: func(request *http.Request) (string, bool) {
			identity := request.Header.Get("X-Test-Principal")
			return identity, identity != ""
		},
	}
	limiter, err := NewRateLimiter(rateConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer limiter.Close()
	resolver, err := NewClientIPResolver(ClientIPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewRateLimitMiddleware(limiter, resolver, rateConfig)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
	}))

	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/pastes", nil)
		request.RemoteAddr = "192.0.2." + strconvForTest(80+attempt) + ":8000"
		request.Header.Set("X-Test-Principal", "verified-token-id")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if attempt == 0 && response.Code != http.StatusCreated {
			t.Fatalf("first create status = %d", response.Code)
		}
		if attempt == 1 && response.Code != http.StatusTooManyRequests {
			t.Fatalf("second create status = %d", response.Code)
		}
	}
}

func TestClientIPResolverRejectsSpoofedForwardingHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.40:4000"
	request.Header.Set("Forwarded", "for=203.0.113.7")
	request.Header.Set("X-Forwarded-For", "203.0.113.8")
	request.Header.Set("X-Real-IP", "203.0.113.9")

	resolver, err := NewClientIPResolver(ClientIPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolver.ClientIP(request); got != "192.0.2.40" {
		t.Fatalf("untrusted forwarded address = %q", got)
	}

	resolver, err = NewClientIPResolver(ClientIPConfig{
		TrustedProxyCIDRs: []string{"192.0.2.0/24"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolver.ClientIP(request); got != "192.0.2.40" {
		t.Fatalf("disabled forwarding address = %q", got)
	}
}

func TestClientIPResolverUsesExplicitTrustedProxyChain(t *testing.T) {
	resolver, err := NewClientIPResolver(ClientIPConfig{
		TrustedProxyCIDRs:     []string{"10.0.0.0/8"},
		TrustForwardedHeaders: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.2:4000"
	request.Header.Set("X-Forwarded-For", "198.51.100.9, 10.0.0.8")
	if got := resolver.ClientIP(request); got != "198.51.100.9" {
		t.Fatalf("client IP = %q", got)
	}

	request.Header.Set("Forwarded", `for="[2001:db8::7]:1234";proto=https`)
	if got := resolver.ClientIP(request); got != "2001:db8::7" {
		t.Fatalf("Forwarded client IP = %q", got)
	}

	request.RemoteAddr = "203.0.113.20:4000"
	if got := resolver.ClientIP(request); got != "203.0.113.20" {
		t.Fatalf("untrusted peer address = %q", got)
	}
}

func TestClientIPResolverRejectsInvalidCIDR(t *testing.T) {
	if _, err := NewClientIPResolver(ClientIPConfig{TrustedProxyCIDRs: []string{"not-a-cidr"}}); err == nil {
		t.Fatal("invalid CIDR was accepted")
	}
}

func TestRateLimiterBoundsAndCleansEntries(t *testing.T) {
	limiter, err := NewRateLimiter(RateLimitConfig{
		AnonymousPerIP:        RateLimitPolicy{Requests: 1000, Window: time.Millisecond, Burst: 10},
		AuthenticatedPerToken: RateLimitPolicy{Requests: 1000, Window: time.Millisecond, Burst: 10},
		MaxIPEntries:          2,
		MaxTokenEntries:       3,
		EntryTTL:              15 * time.Millisecond,
		CleanupInterval:       5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer limiter.Close()

	for index := 0; index < 20; index++ {
		ip := "192.0.2." + strconvForTest(index)
		token := "token-" + strconvForTest(index)
		limiter.Allow(ip, token)
	}
	ipEntries, tokenEntries := limiter.EntryCount()
	if ipEntries > 2 || tokenEntries > 3 {
		t.Fatalf("entry counts = %d, %d", ipEntries, tokenEntries)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ipEntries, tokenEntries = limiter.EntryCount()
		if ipEntries == 0 && tokenEntries == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("periodic cleanup left %d IP and %d token entries", ipEntries, tokenEntries)
}

func strconvForTest(value int) string {
	const digits = "0123456789"
	if value < 10 {
		return string(digits[value])
	}
	return string([]byte{digits[value/10], digits[value%10]})
}

func TestRequestIDMiddleware(t *testing.T) {
	t.Run("generates and propagates", func(t *testing.T) {
		middleware := NewRequestIDMiddleware(RequestIDConfig{
			Enabled:   true,
			Generator: func() string { return "generated-123" },
		})
		handler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte(RequestIDFromContext(request.Context())))
		}))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

		if got := response.Header().Get(defaultRequestIDHeader); got != "generated-123" {
			t.Fatalf("response request ID = %q", got)
		}
		if got := response.Body.String(); got != "generated-123" {
			t.Fatalf("context request ID = %q", got)
		}
	})

	t.Run("accepts only validated incoming values", func(t *testing.T) {
		middleware := NewRequestIDMiddleware(RequestIDConfig{
			Enabled:        true,
			AcceptIncoming: true,
			Generator:      func() string { return "replacement-123" },
		})
		handler := middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		}))

		validRequest := httptest.NewRequest(http.MethodGet, "/", nil)
		validRequest.Header.Set(defaultRequestIDHeader, "upstream:abc-123")
		validResponse := httptest.NewRecorder()
		handler.ServeHTTP(validResponse, validRequest)
		if got := validResponse.Header().Get(defaultRequestIDHeader); got != "upstream:abc-123" {
			t.Fatalf("valid request ID = %q", got)
		}

		invalidRequest := httptest.NewRequest(http.MethodGet, "/", nil)
		invalidRequest.Header.Set(defaultRequestIDHeader, "bad\nvalue")
		invalidResponse := httptest.NewRecorder()
		handler.ServeHTTP(invalidResponse, invalidRequest)
		if got := invalidResponse.Header().Get(defaultRequestIDHeader); got != "replacement-123" {
			t.Fatalf("invalid request ID replacement = %q", got)
		}
	})
}

func TestSecurityPolicyMiddleware(t *testing.T) {
	config := DefaultHTTPMiddlewareConfig()
	resolver, err := NewClientIPResolver(ClientIPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewSecurityPolicyMiddleware(config.SecurityHeaders, resolver)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))

	httpResponse := httptest.NewRecorder()
	handler.ServeHTTP(httpResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := httpResponse.Header().Get("Content-Security-Policy"); got != DefaultContentSecurityPolicy {
		t.Fatalf("CSP = %q", got)
	}
	if got := httpResponse.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := httpResponse.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS was sent over HTTP: %q", got)
	}

	httpsRequest := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	httpsRequest.TLS = &tls.ConnectionState{}
	httpsResponse := httptest.NewRecorder()
	handler.ServeHTTP(httpsResponse, httpsRequest)
	if got := httpsResponse.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("HSTS is missing over HTTPS")
	}
}

func TestSecurityPolicyTrustsForwardedProtoOnlyFromConfiguredProxy(t *testing.T) {
	config := DefaultHTTPMiddlewareConfig()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.2:4000"
	request.Header.Set("X-Forwarded-Proto", "https")

	untrusted, err := NewClientIPResolver(ClientIPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	untrustedResponse := httptest.NewRecorder()
	NewSecurityPolicyMiddleware(config.SecurityHeaders, untrusted)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(untrustedResponse, request)
	if got := untrustedResponse.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("untrusted proxy enabled HSTS: %q", got)
	}

	trusted, err := NewClientIPResolver(ClientIPConfig{
		TrustedProxyCIDRs:     []string{"10.0.0.0/8"},
		TrustForwardedHeaders: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	trustedResponse := httptest.NewRecorder()
	NewSecurityPolicyMiddleware(config.SecurityHeaders, trusted)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(trustedResponse, request)
	if got := trustedResponse.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("trusted HTTPS proxy did not enable HSTS")
	}
}

func TestJSONAccessLogOmitsSensitiveContent(t *testing.T) {
	var output bytes.Buffer
	config := DefaultHTTPMiddlewareConfig()
	config.AccessLog.Writer = &output
	config.RequestID.Generator = func() string { return "log-request-123" }
	config.RequestID.AcceptIncoming = false
	middleware, err := NewHTTPMiddleware(config)
	if err != nil {
		t.Fatal(err)
	}
	defer middleware.Close()

	handler := middleware.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte("response-secret"))
	}))
	request := httptest.NewRequest(http.MethodPost, "/submit?q=query-secret", strings.NewReader("body-secret"))
	request.RemoteAddr = "192.0.2.50:5000"
	request.Header.Set("Authorization", "Bearer token-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if strings.Contains(output.String(), "secret") {
		t.Fatalf("access log contains sensitive content: %s", output.String())
	}
	var record accessLogRecord
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("decode access log: %v", err)
	}
	if record.RequestID != "log-request-123" || record.Path != "/submit" {
		t.Fatalf("unexpected access log: %+v", record)
	}
	if record.Status != http.StatusCreated || record.Bytes != len("response-secret") {
		t.Fatalf("unexpected response metrics: %+v", record)
	}
}

func TestHTTPMiddlewareLogsRateLimitAndAddsSecurityHeaders(t *testing.T) {
	var output bytes.Buffer
	config := DefaultHTTPMiddlewareConfig()
	config.RateLimit.AnonymousPerIP = RateLimitPolicy{Requests: 1, Window: time.Hour, Burst: 1}
	config.RateLimit.AuthenticatedPerToken = RateLimitPolicy{}
	config.AccessLog.Writer = &output
	config.RequestID.Generator = func() string { return "stack-request-123" }
	config.RequestID.AcceptIncoming = false
	middleware, err := NewHTTPMiddleware(config)
	if err != nil {
		t.Fatal(err)
	}
	defer middleware.Close()

	handler := middleware.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	for attempt := 0; attempt < 2; attempt++ {
		request := httptest.NewRequest(http.MethodGet, "/limited", nil)
		request.RemoteAddr = "192.0.2.60:6000"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if attempt == 1 {
			if response.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d", response.Code)
			}
			if response.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("CSP is missing from rate limit response")
			}
			if response.Header().Get(defaultRequestIDHeader) == "" {
				t.Fatal("request ID is missing from rate limit response")
			}
		}
	}
	if lines := bytes.Count(output.Bytes(), []byte("\n")); lines != 2 {
		t.Fatalf("access log lines = %d, want 2", lines)
	}
}

func TestWrapWithAuthenticationRatesInvalidBearerByIP(t *testing.T) {
	type identityContextKey struct{}

	config := DefaultHTTPMiddlewareConfig()
	config.RateLimit.AnonymousPerIP = RateLimitPolicy{Requests: 1, Window: time.Hour, Burst: 1}
	config.RateLimit.AuthenticatedPerToken = RateLimitPolicy{Requests: 2, Window: time.Hour, Burst: 2}
	config.RateLimit.CreatePerIdentity = RateLimitPolicy{}
	config.RateLimit.AuthenticatedIdentity = func(request *http.Request) (string, bool) {
		identity, ok := request.Context().Value(identityContextKey{}).(string)
		return identity, ok
	}
	config.AccessLog.Enabled = false
	middleware, err := NewHTTPMiddleware(config)
	if err != nil {
		t.Fatal(err)
	}
	defer middleware.Close()

	var authenticationCalls atomic.Int64
	authentication := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			authenticationCalls.Add(1)
			switch request.Header.Get("Authorization") {
			case "Bearer valid":
				ctx := context.WithValue(request.Context(), identityContextKey{}, "verified-token-id")
				next.ServeHTTP(writer, request.WithContext(ctx))
			case "Bearer invalid":
				http.Error(writer, "invalid token", http.StatusUnauthorized)
			default:
				next.ServeHTTP(writer, request)
			}
		})
	}
	handler := middleware.WrapWithAuthentication(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}), authentication)

	request := httptest.NewRequest(http.MethodGet, "/api/pastes", nil)
	request.RemoteAddr = "192.0.2.61:6000"
	request.Header.Set("Authorization", "Bearer invalid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("first invalid token status = %d", response.Code)
	}

	for attempt := 0; attempt < 2; attempt++ {
		request = httptest.NewRequest(http.MethodGet, "/api/pastes", nil)
		request.RemoteAddr = "192.0.2.61:6000"
		request.Header.Set("Authorization", "Bearer valid")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("valid token attempt %d status = %d", attempt+1, response.Code)
		}
	}

	request = httptest.NewRequest(http.MethodGet, "/api/pastes", nil)
	request.RemoteAddr = "192.0.2.61:6000"
	request.Header.Set("Authorization", "Bearer valid")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("authenticated limit status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/pastes", nil)
	request.RemoteAddr = "192.0.2.61:6000"
	request.Header.Set("Authorization", "Bearer invalid")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("second invalid token status = %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "invalid token") {
		t.Fatalf("rate limit response exposed the authentication response: %q", response.Body.String())
	}
	if got := authenticationCalls.Load(); got != 5 {
		t.Fatalf("authentication calls = %d, want 5", got)
	}
}

func TestRateLimiterConcurrentAccess(t *testing.T) {
	limiter, err := NewRateLimiter(RateLimitConfig{
		AnonymousPerIP:  RateLimitPolicy{Requests: 50, Window: time.Hour, Burst: 50},
		MaxIPEntries:    32,
		MaxTokenEntries: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer limiter.Close()

	var allowed atomic.Int64
	var waitGroup sync.WaitGroup
	for index := 0; index < 200; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if accepted, _ := limiter.Allow("192.0.2.70", ""); accepted {
				allowed.Add(1)
			}
			limiter.EntryCount()
			limiter.Cleanup()
		}()
	}
	waitGroup.Wait()
	if got := allowed.Load(); got != 50 {
		t.Fatalf("allowed requests = %d, want 50", got)
	}
}

func TestConcurrentWorkLimiter(t *testing.T) {
	t.Run("times out and releases", func(t *testing.T) {
		limiter := newConcurrentWorkLimiter(1, 10*time.Millisecond)
		if !limiter.acquire(context.Background()) {
			t.Fatal("first operation was rejected")
		}
		if limiter.acquire(context.Background()) {
			t.Fatal("second operation exceeded the limit")
		}
		limiter.release()
		if !limiter.acquire(context.Background()) {
			t.Fatal("released capacity was not available")
		}
		limiter.release()
	})

	t.Run("rejects canceled work", func(t *testing.T) {
		limiter := newConcurrentWorkLimiter(1, time.Second)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if limiter.acquire(ctx) {
			t.Fatal("a canceled operation acquired capacity")
		}
	})

	t.Run("never exceeds configured concurrency", func(t *testing.T) {
		limiter := newConcurrentWorkLimiter(3, time.Second)
		var active atomic.Int64
		var maximum atomic.Int64
		var waitGroup sync.WaitGroup
		for index := 0; index < 30; index++ {
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				if !limiter.acquire(context.Background()) {
					t.Error("operation timed out")
					return
				}
				current := active.Add(1)
				for {
					observed := maximum.Load()
					if current <= observed || maximum.CompareAndSwap(observed, current) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				active.Add(-1)
				limiter.release()
			}()
		}
		waitGroup.Wait()
		if got := maximum.Load(); got > 3 {
			t.Fatalf("maximum concurrency = %d", got)
		}
	})

	t.Run("wakes all capacity after a limit increase", func(t *testing.T) {
		limiter := newConcurrentWorkLimiter(1, time.Second)
		if !limiter.acquire(context.Background()) {
			t.Fatal("initial operation was rejected")
		}

		started := make(chan struct{}, 2)
		acquired := make(chan struct{}, 2)
		releaseWaiters := make(chan struct{})
		var releaseOnce sync.Once
		releaseAll := func() {
			releaseOnce.Do(func() { close(releaseWaiters) })
		}
		defer releaseAll()
		for index := 0; index < 2; index++ {
			go func() {
				started <- struct{}{}
				if limiter.acquire(context.Background()) {
					acquired <- struct{}{}
					<-releaseWaiters
					limiter.release()
				}
			}()
		}
		<-started
		<-started
		time.Sleep(10 * time.Millisecond)
		limiter.configure(3, time.Second)

		for index := 0; index < 2; index++ {
			select {
			case <-acquired:
			case <-time.After(200 * time.Millisecond):
				t.Fatal("a waiting operation did not use the increased capacity")
			}
		}
		releaseAll()
		limiter.release()
	})
}

func TestConfigureWorkLimits(t *testing.T) {
	t.Cleanup(func() {
		if err := ConfigureWorkLimits(DefaultWorkLimitConfig()); err != nil {
			t.Errorf("restore work limits: %v", err)
		}
	})
	config := WorkLimitConfig{DiffLimit: 1, FormatLimit: 2, PreviewLimit: 3, WaitTimeout: 0}
	if err := ConfigureWorkLimits(config); err != nil {
		t.Fatal(err)
	}
	stats := WorkLimitStats()
	if stats["diff"].Limit != 1 || stats["format"].Limit != 2 || stats["preview"].Limit != 3 {
		t.Fatalf("unexpected work limits: %+v", stats)
	}
	if err := ConfigureWorkLimits(WorkLimitConfig{}); err == nil {
		t.Fatal("invalid work limits were accepted")
	}
}
