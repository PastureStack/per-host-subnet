package metadata

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 8 << 20

type Client interface {
	SelfHost(context.Context) (Host, error)
	Hosts(context.Context) ([]Host, error)
	Networks(context.Context) ([]Network, error)
	Containers(context.Context) ([]Container, error)
	Ready(context.Context) error
	Watch(context.Context, time.Duration, func(context.Context) error) error
}

type HTTPClient struct {
	baseURL *url.URL
	client  *http.Client
}

func NewClient(rawURL, caRootPath string) (*HTTPClient, error) {
	parsed, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid metadata URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("metadata URL must not contain credentials, query, or fragment")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if caRootPath != "" {
		pem, err := os.ReadFile(caRootPath)
		if err != nil {
			return nil, fmt.Errorf("read metadata CA root: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("metadata CA root contains no certificates")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	return &HTTPClient{
		baseURL: parsed,
		client: &http.Client{
			Timeout:   15 * time.Second,
			Transport: transport,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) >= 3 || !sameAuthority(request.URL, parsed) {
					return fmt.Errorf("metadata redirect rejected")
				}
				return nil
			},
		},
	}, nil
}

func sameAuthority(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func (client *HTTPClient) endpoint(resource string, query url.Values) string {
	target := *client.baseURL
	target.Path = strings.TrimRight(client.baseURL.Path, "/") + "/" + strings.TrimLeft(resource, "/")
	target.RawQuery = query.Encode()
	return target.String()
}

func (client *HTTPClient) get(ctx context.Context, resource string, query url.Values, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.endpoint(resource, query), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("metadata resource %q returned %s", resource, response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("metadata resource %q exceeds %d bytes", resource, maxResponseBytes)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode metadata resource %q: %w", resource, err)
	}
	return nil
}

func (client *HTTPClient) SelfHost(ctx context.Context) (value Host, err error) {
	err = client.get(ctx, "self/host", nil, &value)
	return
}

func (client *HTTPClient) Hosts(ctx context.Context) (value []Host, err error) {
	err = client.get(ctx, "hosts", nil, &value)
	return
}

func (client *HTTPClient) Networks(ctx context.Context) (value []Network, err error) {
	err = client.get(ctx, "networks", nil, &value)
	return
}

func (client *HTTPClient) Containers(ctx context.Context) (value []Container, err error) {
	err = client.get(ctx, "containers", nil, &value)
	return
}

func (client *HTTPClient) version(ctx context.Context, current string, wait time.Duration) (string, error) {
	query := url.Values{}
	if current != "" {
		query.Set("wait", "true")
		query.Set("value", current)
		query.Set("maxWait", strconv.Itoa(max(1, int(wait/time.Second))))
	}
	var version string
	err := client.get(ctx, "version", query, &version)
	return version, err
}

func (client *HTTPClient) Ready(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := client.version(ctx, "", 0); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("metadata readiness timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (client *HTTPClient) Watch(ctx context.Context, wait time.Duration, callback func(context.Context) error) error {
	if wait <= 0 {
		return fmt.Errorf("metadata watch interval must be positive")
	}
	current := ""
	for {
		version, err := client.version(ctx, current, wait)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("metadata version check failed", "error", err)
			if err := waitForContext(ctx, wait); err != nil {
				return nil
			}
			continue
		}
		if version != current {
			current = version
			if err := callback(ctx); err != nil {
				slog.Error("metadata change callback failed", "error", err)
			}
		}
	}
}

func waitForContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
