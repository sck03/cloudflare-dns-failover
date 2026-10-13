package health

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPProbesOriginWithWebsiteHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Host, "website.invalid:") {
			t.Errorf("Host = %q", r.Host)
		}
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/health", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	checker := NewChecker()
	defer checker.Close()
	spec := Spec{Type: "http", Target: "http://website.invalid:" + port + "/start", Timeout: time.Second, ExpectedStatus: 204}
	if err := checker.Check(context.Background(), spec, host); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPSUsesSNIAndRequiresExplicitInsecureOptIn(t *testing.T) {
	sni := make(chan string, 2)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		sni <- hello.ServerName
		return nil, nil
	}}
	server.StartTLS()
	defer server.Close()
	host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	checker := NewChecker()
	defer checker.Close()
	spec := Spec{Type: "https", Target: "website.invalid:" + port, Timeout: time.Second}
	if err := checker.Check(context.Background(), spec, host); err == nil {
		t.Fatal("self-signed origin certificate was accepted by default")
	}
	spec.InsecureSkipVerify = true
	if err := checker.Check(context.Background(), spec, host); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if got := <-sni; got != "website.invalid" {
			t.Fatalf("SNI = %q", got)
		}
	}
}

func TestRejectsRedirectToUnrelatedHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://other.invalid/", http.StatusFound)
	}))
	defer server.Close()
	host, _, _ := net.SplitHostPort(server.Listener.Addr().String())
	checker := NewChecker()
	defer checker.Close()
	err := checker.Check(context.Background(), Spec{Type: "http", Target: server.URL, Timeout: time.Second}, host)
	if err == nil || !strings.Contains(err.Error(), "another host") {
		t.Fatalf("got %v, want cross-host redirect rejection", err)
	}
}

func TestApplicationErrorsAndTruncatedResponsesAreUnhealthy(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			host, _, _ := net.SplitHostPort(server.Listener.Addr().String())
			checker := NewChecker()
			defer checker.Close()
			if err := checker.Check(context.Background(), Spec{Type: "http", Target: server.URL, Timeout: time.Second}, host); err == nil {
				t.Fatal("application failure was considered healthy")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("truncated"))
	}))
	defer server.Close()
	host, _, _ := net.SplitHostPort(server.Listener.Addr().String())
	checker := NewChecker()
	defer checker.Close()
	if err := checker.Check(context.Background(), Spec{Type: "http", Target: server.URL, Timeout: time.Second}, host); err == nil {
		t.Fatal("truncated response was considered healthy")
	}
}

func TestCheckCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	host, _, _ := net.SplitHostPort(server.Listener.Addr().String())
	checker := NewChecker()
	defer checker.Close()
	start := time.Now()
	err := checker.Check(context.Background(), Spec{Type: "http", Target: server.URL, Timeout: 30 * time.Millisecond}, host)
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("probe did not respect deadline: %v", err)
	}
}

func TestConcurrentOriginsNeverShareConnections(t *testing.T) {
	// Use two loopback addresses on the same port to model one hostname with two origins.
	first := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	first.Start()
	defer first.Close()
	_, port, _ := net.SplitHostPort(first.Listener.Addr().String())
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.2", port))
	if err != nil {
		t.Skipf("second loopback address unavailable: %v", err)
	}
	second := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	second.Listener.Close()
	second.Listener = listener
	second.Start()
	defer second.Close()
	checker := NewChecker()
	defer checker.Close()
	spec := Spec{Type: "http", Target: "http://website.invalid:" + port, Timeout: time.Second}
	for range 10 {
		primary := make(chan error, 1)
		go func() { primary <- checker.Check(context.Background(), spec, "127.0.0.1") }()
		backupErr := checker.Check(context.Background(), spec, "127.0.0.2")
		if err := <-primary; err != nil {
			t.Fatal(err)
		}
		if backupErr == nil {
			t.Fatal("backup reused the primary connection")
		}
	}
}

func TestWindowsPingDoesNotAcceptUnreachableReplies(t *testing.T) {
	for _, tc := range []struct {
		output  string
		healthy bool
	}{
		{"Reply from 127.0.0.1: bytes=32 time<1ms TTL=128", true},
		{"来自 ::1 的回复: 时间<1ms", true},
		{"Reply from 192.0.2.1: Destination host unreachable.\nPackets: Sent = 1, Received = 1, Lost = 0", false},
		{"来自 192.0.2.1 的回复: 无法访问目标主机。", false},
		{"Request timed out.\nMinimum = 0ms, Maximum = 0ms, Average = 0ms", false},
	} {
		if got := windowsEchoReply.MatchString(tc.output); got != tc.healthy {
			t.Errorf("ping output %q: healthy=%v", tc.output, got)
		}
	}
}
