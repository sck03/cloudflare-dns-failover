package cloudflare

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

var testCredentials = Credentials{Token: "test-token"}

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := NewClient()
	c.BaseURL, c.HTTP = server.URL, server.Client()
	return c
}

func TestValidatesResponseEnvelope(t *testing.T) {
	for _, body := range []string{`{"success":false,"errors":[{"message":"rejected"}]}`, `{`, `{"success":true,"result":null}`} {
		t.Run(body, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			if _, err := client.Record(context.Background(), testCredentials, "zone", "record"); err == nil {
				t.Fatal("invalid API response was accepted")
			}
		})
	}
}

func TestPaginationAndAuthentication(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing token")
		}
		if r.URL.Query().Get("per_page") != "50" {
			t.Error("invalid zone page size")
		}
		fmt.Fprintf(w, `{"success":true,"result":[{"id":"%s"}],"result_info":{"total_pages":2}}`, r.URL.Query().Get("page"))
	})
	zones, err := client.Zones(context.Background(), testCredentials)
	if err != nil || len(zones) != 2 || calls.Load() != 2 || zones[1].ID != "2" {
		t.Fatalf("pagination: zones=%v calls=%d err=%v", zones, calls.Load(), err)
	}
}

func TestRateLimitBackoffIsCancelableAndShared(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"success":false,"errors":[{"message":"rate limited"}]}`)
	})
	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_, err := client.Record(ctx, testCredentials, "zone", "record")
		cancel()
		if err == nil {
			t.Fatal("rate limit accepted")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("shared rate limit ignored: %d requests", calls.Load())
	}
}

func TestCreateIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"success":false}`)
	})
	_, err := client.SaveRecord(context.Background(), testCredentials, "zone", "", map[string]string{"type": "A"})
	if err == nil || calls.Load() != 1 {
		t.Fatalf("POST was retried: calls=%d err=%v", calls.Load(), err)
	}
}

func TestTransientPatchFailureRetries(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"success":false}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"result":{"id":"record","content":"192.0.2.2"}}`)
	})
	record, err := client.SaveRecord(context.Background(), testCredentials, "zone", "record", map[string]string{"content": "192.0.2.2"})
	if err != nil || record.Content != "192.0.2.2" || calls.Load() != 2 {
		t.Fatalf("retry: record=%v calls=%d err=%v", record, calls.Load(), err)
	}
}
