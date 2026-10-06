package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type identityKey struct{}
type identity struct{ username string }

// Proxy forwards HTTP requests and validates CAS personal access tokens.
type Proxy struct {
	config    Config
	logger    *slog.Logger
	transport *http.Transport
	target    *url.URL
	casClient *http.Client
}

// NewProxy creates a proxy using the supplied configuration and logger.
func NewProxy(c Config, logger *slog.Logger) *Proxy {
	if logger == nil {
		logger = slog.Default()
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Environment HTTP proxies must not intercept local upstream traffic.
	transport.Proxy = nil
	if c.UpstreamSocket != "" {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", c.UpstreamSocket)
		}
	}
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort(c.UpstreamHost, strconv.Itoa(c.UpstreamPort))}
	casTransport := http.DefaultTransport.(*http.Transport).Clone()
	casTransport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.CASInsecure} // explicitly configured for CAS only
	casClient := &http.Client{
		Transport: casTransport, Timeout: time.Duration(c.ValidationTimeout) * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Proxy{config: c, logger: logger, transport: transport, target: target, casClient: casClient}
}

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// Run starts the proxy and shuts it down on SIGINT or SIGTERM.
func (p *Proxy) Run() error {
	c := p.config
	if err := c.validate(); err != nil {
		return err
	}
	// pass this proxy as Handler for the listen Address
	server := &http.Server{Addr: c.ListenAddress, Handler: p,
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, ErrorLog: discardLogger()}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	p.logger.Info("proxy", "event", "starting", "listenAddress", c.ListenAddress)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("proxy listener failed")
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
			return errors.New("proxy shutdown timed out")
		}
		return nil
	}
}

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c := p.config
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	id := hex.EncodeToString(idBytes)
	started := time.Now()
	logEvent := func(event string, args ...any) {
		fields := []any{"event", event, "requestId", id, "elapsedMs", time.Since(started).Milliseconds()}
		p.logger.Info("proxy", append(fields, args...)...)
	}
	fail := func(status int) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Pat-Proxy-Request-Id", id)
		http.Error(w, http.StatusText(status), status)
	}

	// remove x-intenal header for clean request
	stripInternalHeaders(r.Header)

	// get username from request and check if it is pat_ password request
	if username, matches := patUsername(r, c.APIPath); c.ValidationEnabled && matches {
		done := p.validate(r, c, logEvent, fail, username)
		// if done, no further redirect will create an dogu token
		if done {
			return
		}
	}

	// if request is not already done, we got a valid pat-request
	// this must be redirected to dogu-proxy
	proxy := &httputil.ReverseProxy{
		Transport: p.transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Preserve the original path, query and Host, including encoded path segments.
			pr.Out.URL.Scheme, pr.Out.URL.Host = p.target.Scheme, p.target.Host
			pr.Out.Host = pr.In.Host
			// ReverseProxy removes untrusted forwarding headers before Rewrite.
			// Preserve the Node proxy's existing forwarding metadata contract.
			for _, name := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
				if values := pr.In.Header.Values(name); len(values) > 0 {
					pr.Out.Header[name] = append([]string(nil), values...)
				}
			}
			stripInternalHeaders(pr.Out.Header)
			if auth, ok := pr.In.Context().Value(identityKey{}).(identity); ok {
				pr.Out.Header.Del("Authorization")
				pr.Out.Header.Set("X-Internal-Auth-Method", "cas-pat")
				pr.Out.Header.Set("X-Internal-Auth-User", base64.StdEncoding.EncodeToString([]byte(auth.username)))
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			logEvent("upstream_connection_error", "responseStatus", 502)
			fail(http.StatusBadGateway)
		},
		ModifyResponse: func(response *http.Response) error {
			if response.StatusCode >= 400 {
				logEvent("upstream_response", "upstreamStatus", response.StatusCode)
			}
			return nil
		},
		// The default logger can include URLs or raw error messages.
		ErrorLog: discardLogger(),
	}
	proxy.ServeHTTP(w, r)
}

func (p *Proxy) validate(r *http.Request, c Config, logEvent func(event string, args ...any), fail func(status int), username string) bool {
	validation, err := http.NewRequestWithContext(r.Context(), http.MethodGet, c.CASURL, nil)
	if err != nil {
		logEvent("cas_validation_error")
		fail(http.StatusBadGateway)
		return true
	}
	if c.Scope != "" {
		query := validation.URL.Query()
		query.Del("scope")
		validation.URL.RawQuery = query.Encode()
		if validation.URL.RawQuery != "" {
			validation.URL.RawQuery += "&"
		}
		validation.URL.RawQuery += "scope=" + url.QueryEscape(c.Scope)
	}
	validation.Header.Set("Authorization", r.Header.Get("Authorization"))
	response, err := p.casClient.Do(validation)
	if err != nil {
		code := "CAS_CONNECTION_ERROR"
		if e, ok := err.(net.Error); ok && e.Timeout() {
			code = "CAS_TIMEOUT"
		}
		logEvent("cas_validation_error", "code", code, "responseStatus", 502)
		fail(http.StatusBadGateway)
		return true
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		status := http.StatusBadGateway
		if response.StatusCode == 400 || response.StatusCode == 401 {
			status = response.StatusCode
		}
		logEvent("cas_validation_rejected", "casStatus", response.StatusCode, "responseStatus", status)
		fail(status)
		return true
	}
	logEvent("cas_validation_ok", "casStatus", 200)
	*r = *r.WithContext(context.WithValue(r.Context(), identityKey{}, identity{username}))
	return false
}

func stripInternalHeaders(h http.Header) {
	for name := range h {
		if strings.HasPrefix(strings.ToLower(name), "x-internal-auth-") {
			delete(h, name)
		}
	}
}

func patUsername(r *http.Request, apiPath string) (string, bool) {
	path := r.URL.EscapedPath()
	if apiPath != "/" && path != apiPath && !strings.HasPrefix(path, strings.TrimSuffix(apiPath, "/")+"/") {
		return "", false
	}
	auth := r.Header.Get("Authorization")
	scheme, encoded, found := strings.Cut(auth, " ")
	if !found || !strings.EqualFold(scheme, "Basic") {
		return "", false
	}
	credentials, err := base64.StdEncoding.DecodeString(strings.TrimLeft(encoded, " "))
	if err != nil {
		return "", false
	}
	username, password, found := strings.Cut(string(credentials), ":")
	return username, found && strings.HasPrefix(password, "pat_")
}
