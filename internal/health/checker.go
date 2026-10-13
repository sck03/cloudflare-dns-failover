// Package health probes an origin while preserving the website's HTTP Host and TLS SNI.
package health

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Spec struct {
	Type, Target       string
	Timeout            time.Duration
	PingCount          int
	InsecureSkipVerify bool
	ExpectedStatus     int
}

type originKey struct{}

// Windows ping can exit successfully for an ICMP destination-unreachable reply.
// Require an actual echo response, independently of the localized surrounding text.
var windowsEchoReply = regexp.MustCompile(`(?im)(TTL=\d+|:[^\r\n]*[=<]\s*\d+\s*ms)`)

type Checker struct {
	secure, insecure *http.Transport
}

func NewChecker() *Checker {
	transport := &http.Transport{
		// Fresh origin connections prevent a pooled connection from hiding a failed
		// endpoint or being reused for a different origin of the same website.
		DisableKeepAlives:      true,
		TLSHandshakeTimeout:    10 * time.Second,
		MaxResponseHeaderBytes: 32 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			origin, _ := ctx.Value(originKey{}).(string)
			if origin == "" {
				return nil, errors.New("missing origin address")
			}
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(origin, port))
		},
	}
	insecure := transport.Clone()
	insecure.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // Explicit per-monitor opt-in for private origin certificates.
	return &Checker{secure: transport, insecure: insecure}
}

func (c *Checker) Close() {
	c.secure.CloseIdleConnections()
	c.insecure.CloseIdleConnections()
}

func (c *Checker) Check(ctx context.Context, spec Spec, origin string) error {
	if spec.Timeout <= 0 {
		return errors.New("check timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()
	switch spec.Type {
	case "ping":
		return checkPing(ctx, origin, spec.PingCount)
	case "http", "https":
		return c.checkHTTP(ctx, spec, origin)
	default:
		return fmt.Errorf("unsupported check type %q", spec.Type)
	}
}

func (c *Checker) checkHTTP(ctx context.Context, spec Spec, origin string) error {
	target, err := ParseTarget(spec.Type, spec.Target)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, originKey{}, origin)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "CFGuard-Monitor")
	req.Header.Set("Cache-Control", "no-cache")
	transport := c.secure
	if spec.InsecureSkipVerify {
		transport = c.insecure
	}
	client := http.Client{
		Transport: transport,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many health check redirects")
			}
			if !strings.EqualFold(next.URL.Hostname(), target.Hostname()) ||
				(next.URL.Scheme != "http" && next.URL.Scheme != "https") {
				return errors.New("health check redirected to another host; configure a direct health endpoint")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if spec.ExpectedStatus != 0 && resp.StatusCode != spec.ExpectedStatus ||
		spec.ExpectedStatus == 0 && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// Bound traffic even when a user accidentally selects a download endpoint.
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 32<<10))
	return err
}

func ParseTarget(kind, target string) (*url.URL, error) {
	if !strings.Contains(target, "://") {
		target = kind + "://" + target
	}
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("invalid health URL: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("health URL must be HTTP/HTTPS without credentials or a fragment")
	}
	return u, nil
}

func checkPing(ctx context.Context, host string, count int) error {
	if host == "" || strings.HasPrefix(host, "-") {
		return errors.New("invalid ping host")
	}
	var lastErr error
	for range max(count, 1) {
		if err := ctx.Err(); err != nil {
			return err
		}
		deadline, _ := ctx.Deadline()
		remaining := time.Until(deadline)
		args := []string{"-c", "1", "-W", strconv.Itoa(max(int(remaining.Seconds()), 1)), host}
		if runtime.GOOS == "windows" {
			args = []string{"-n", "1", "-w", strconv.FormatInt(max(remaining.Milliseconds(), 1), 10), host}
		} else if runtime.GOOS == "darwin" {
			args = []string{"-c", "1", "-W", strconv.FormatInt(max(remaining.Milliseconds(), 1), 10), host}
		}
		command := exec.CommandContext(ctx, "ping", args...)
		if runtime.GOOS == "windows" {
			var output []byte
			output, lastErr = command.Output()
			if lastErr == nil && !windowsEchoReply.Match(output) {
				lastErr = errors.New("ping returned no echo reply")
			}
		} else {
			lastErr = command.Run()
		}
		if lastErr == nil {
			return nil
		}
	}
	return fmt.Errorf("ping failed: %w", lastErr)
}
