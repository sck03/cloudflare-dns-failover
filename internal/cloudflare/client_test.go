package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecordFiltersCanBeReusedConcurrently(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "site.example" || r.URL.Query().Get("per_page") != "100" {
			t.Error("record filter changed")
		}
		fmt.Fprint(w, `{"success":true,"result":[],"result_info":{"total_pages":1}}`)
	})
	filter := url.Values{"name": {"site.example"}, "page": {"7"}, "per_page": {"5"}}
	var group sync.WaitGroup
	for range 10 {
		group.Go(func() {
			if _, err := client.Records(context.Background(), testCredentials, "zone", filter); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if !reflect.DeepEqual(filter, url.Values{"name": {"site.example"}, "page": {"7"}, "per_page": {"5"}}) {
		t.Fatalf("caller's query was mutated: %v", filter)
	}
}

var testCredentials = Credentials{Token: "test-token"}

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := NewClient()
	c.BaseURL = server.URL
	return c
}

func TestAPIRedirectDoesNotForwardCredentialsOrReplayWrites(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		fmt.Fprint(w, `{"success":true,"result":{"id":"record"}}`)
	}))
	defer destination.Close()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	})
	for _, auth := range []Credentials{testCredentials, {Email: "admin@example.com", Key: "private-key"}} {
		_, err := client.SaveRecord(context.Background(), auth, "zone", "record", map[string]string{"content": "192.0.2.1"})
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTemporaryRedirect {
			t.Fatalf("unexpected redirect result: %v", err)
		}
	}
	if redirected.Load() != 0 {
		t.Fatal("redirect destination received credentials or a DNS mutation")
	}
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

func TestRetryAfterDateAndSecondsUseTheSameBound(t *testing.T) {
	for _, value := range []string{"999999", time.Now().Add(24 * time.Hour).UTC().Format(http.TimeFormat)} {
		if got := retryDelay(value); got != time.Hour {
			t.Errorf("%q: delay %v, want one hour", value, got)
		}
	}
	for _, value := range []string{"-1", "invalid", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)} {
		if got := retryDelay(value); got != 0 {
			t.Errorf("%q: delay %v, want zero", value, got)
		}
	}
}
