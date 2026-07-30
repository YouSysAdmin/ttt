// Package client implements the store interfaces over the HTTP/JSON API
// served by internal/remote/server, so handlers, the CLI, and the TUI work
// against a remote ttt server without changes. Errors decode back to the
// sentinels in internal/core/errs, and records mutated server-side (Upsert
// timestamps, Notes.Add CreatedAt bumps) are copied back into the caller's
// pointers to preserve the stores' in-place contracts.
package client

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	ttls "ttt/internal/core/tls"
	"ttt/internal/remote/api"
)

// Options configures the connection to a ttt server.
type Options struct {
	// Token is sent as the bearer token on every request.
	Token string
	// Insecure skips server certificate verification (self-signed servers).
	Insecure bool
	// CertFile/KeyFile/CAFile enable mutual TLS: the client presents the
	// keypair and verifies the server against the CA bundle.
	CertFile string
	KeyFile  string
	CAFile   string
	// Timeout bounds every request end-to-end. Defaults to 10s.
	Timeout time.Duration
	// CacheTTL enables the read snapshot cache: reads are served from one
	// /v1/state fetch at most this old, and writes invalidate it. 0 disables
	// the cache (every read is its own request).
	CacheTTL time.Duration
}

// Client is the HTTP transport shared by the four store implementations. It
// also owns the read snapshot cache (see snapshot.go).
type Client struct {
	baseURL string
	token   string
	hc      *http.Client

	ttl             time.Duration
	mu              sync.Mutex
	snap            *snapshot
	snapUnsupported bool
}

// New builds a client for the server at baseURL (scheme required, https for
// TLS servers).
func New(baseURL string, opts Options) (*Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return nil, fmt.Errorf("remote url %q: scheme must be http or https", baseURL)
	}

	tlsCfg, err := clientTLS(opts)
	if err != nil {
		return nil, err
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		baseURL: baseURL,
		token:   opts.Token,
		// No zero-defaulting for the TTL - 0 means the cache is off.
		ttl: opts.CacheTTL,
		hc: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}, nil
}

// clientTLS resolves the TLS side of Options: mutual TLS when a keypair and
// CA are given, plain verification otherwise, with Insecure disabling
// verification either way.
func clientTLS(opts Options) (*tls.Config, error) {
	var cfg *tls.Config
	if opts.CertFile != "" || opts.KeyFile != "" || opts.CAFile != "" {
		_, clientCfg, err := ttls.LoadMutualTLS(ttls.MutualTLS{
			CertFile: opts.CertFile,
			KeyFile:  opts.KeyFile,
			CAFile:   opts.CAFile,
		})
		if err != nil {
			return nil, fmt.Errorf("remote tls: %w", err)
		}
		cfg = clientCfg
	}
	if opts.Insecure {
		if cfg == nil {
			cfg = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		cfg.InsecureSkipVerify = true
	}
	return cfg, nil
}

// Ping checks connectivity and auth, returning the server's version.
func (c *Client) Ping() (string, error) {
	var resp api.PingResp
	if err := c.post("/v1/ping", api.Empty{}, &resp); err != nil {
		return "", err
	}
	return resp.Version, nil
}

// post sends one request/response round-trip. Non-2xx responses decode the
// error envelope back to a sentinel (or an opaque remote error) via
// api.DecodeError.
func (c *Client) post(path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("remote: encode request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("remote: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("remote: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var env api.ErrorEnvelope
		if derr := json.NewDecoder(resp.Body).Decode(&env); derr != nil || env.Error.Code == "" {
			// No envelope - typed so callers can branch on the raw status
			// (the snapshot fetch detects old servers by their bare 404).
			return &statusError{status: resp.StatusCode, path: path, text: resp.Status}
		}
		return api.DecodeError(env.Error.Code, env.Error.Message)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("remote: decode response: %w", err)
	}
	return nil
}

// statusError is a non-2xx response that carried no error envelope.
type statusError struct {
	status int
	path   string
	text   string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("remote: %s %s: %s", http.MethodPost, e.path, e.text)
}
