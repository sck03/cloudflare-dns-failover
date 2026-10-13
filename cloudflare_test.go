package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"cfguard/internal/cloudflare"
)

func cloudflareServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfClient = cloudflare.NewClient()
	cfClient.BaseURL, cfClient.HTTP = server.URL, server.Client()
}

func cfResponse(w http.ResponseWriter, result any) {
	json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
}

func TestDNSPatchRequiresMatchingConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*CloudflareRecord)
	}{
		{"wrong content", func(r *CloudflareRecord) { r.Content = "192.0.2.99" }},
		{"wrong proxy", func(r *CloudflareRecord) { r.Proxied = false }},
		{"wrong ID", func(r *CloudflareRecord) { r.ID = "other" }},
		{"wrong name", func(r *CloudflareRecord) { r.Name = "other.example" }},
		{"wrong type", func(r *CloudflareRecord) { r.Type = "AAAA" }},
		{"wrong TTL", func(r *CloudflareRecord) { r.TTL = 300 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testDatabase(t)
			m := createTestMonitor(t)
			m.BackupIPCDNEnabled, m.DNSTTL = true, 60
			cloudflareServer(t, func(w http.ResponseWriter, r *http.Request) {
				record := CloudflareRecord{ID: "record", Name: m.CFDomain, Type: "A", Content: m.OriginalIP, TTL: 300}
				if r.Method == http.MethodPatch {
					record.Content, record.Proxied, record.TTL = m.BackupIP, true, 1
					tc.mutate(&record)
				}
				cfResponse(w, record)
			})
			if _, err := updateDNS(context.Background(), &m, m.BackupIP); err == nil {
				t.Fatal("incorrect DNS confirmation accepted")
			}
		})
	}
}

func TestDNSPatchRejectsSuccessFalse(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	cloudflareServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			w.Write([]byte(`{"success":false,"errors":[{"message":"rejected"}]}`))
			return
		}
		cfResponse(w, CloudflareRecord{ID: "record", Name: m.CFDomain, Type: "A", Content: m.OriginalIP})
	})
	if _, err := updateDNS(context.Background(), &m, m.BackupIP); err == nil {
		t.Fatal("failed PATCH was accepted")
	}
}

func TestDNSPatchOnlyChangesFailoverFields(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	m.DNSTTL = 60
	cloudflareServer(t, func(w http.ResponseWriter, r *http.Request) {
		record := CloudflareRecord{ID: "record", Name: m.CFDomain, Type: "A", Content: m.OriginalIP, TTL: 300}
		if r.Method == http.MethodPatch {
			var payload map[string]any
			json.NewDecoder(r.Body).Decode(&payload)
			if len(payload) != 3 || payload["name"] != nil || payload["type"] != nil || payload["content"] != m.BackupIP {
				t.Errorf("unexpected DNS payload: %v", payload)
			}
			record.Content, record.TTL = m.BackupIP, 60
		}
		cfResponse(w, record)
	})
	from, err := updateDNS(context.Background(), &m, m.BackupIP)
	if err != nil || from != m.OriginalIP {
		t.Fatalf("switch: from=%q, err=%v", from, err)
	}
}

func TestTTLIsPreparedWithoutChangingLiveProxyOrOrigin(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	m.DNSTTL, m.OriginalIPCDNEnabled = 60, true
	record := CloudflareRecord{ID: "record", Name: m.CFDomain, Type: "A", Content: m.OriginalIP, TTL: 3600, Proxied: false}
	cloudflareServer(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if len(payload) != 1 || payload["ttl"] != float64(60) {
			t.Errorf("TTL update changed extra fields: %v", payload)
		}
		updated := record
		updated.TTL = 60
		cfResponse(w, updated)
	})
	if err := (cloudflareDNS{}).ConfigureTTL(context.Background(), &m, &record); err != nil {
		t.Fatal(err)
	}
}

func TestStaleIDRecoveryAndAmbiguity(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	m.CFRecordID = "stale"
	var ambiguous atomic.Bool
	cloudflareServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/stale") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"success":false,"errors":[{"code":81044,"message":"record does not exist"}]}`))
			return
		}
		records := []CloudflareRecord{{ID: "new", Name: m.CFDomain, Type: "A", Content: m.BackupIP}}
		if ambiguous.Load() {
			records = append(records, CloudflareRecord{ID: "other", Name: m.CFDomain, Type: "A", Content: m.OriginalIP})
		}
		cfResponse(w, records)
	})
	if record, err := readDNSRecord(context.Background(), &m); err != nil || record.ID != "new" || m.CFRecordID != "new" {
		t.Fatalf("stale ID was not repaired: %v %v", record, err)
	}
	ambiguous.Store(true)
	m.CFRecordID = ""
	if _, err := readDNSRecord(context.Background(), &m); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("ambiguous records were accepted: %v", err)
	}
}

func TestSwitchRechecksRemoteAddressBeforeWriting(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	m.CurrentIP = m.OriginalIP
	cloudflareServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("attempted to overwrite an unexpected remote address")
		}
		cfResponse(w, CloudflareRecord{ID: "record", Name: m.CFDomain, Type: "A", Content: "192.0.2.99"})
	})
	if _, err := updateDNS(context.Background(), &m, m.BackupIP); err == nil {
		t.Fatal("unknown remote content accepted")
	}
}
