package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCanceledManagementRequestDoesNotMutateConfiguration(t *testing.T) {
	testDatabase(t)
	unlock, err := managementLocks.Lock(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/api/config", strings.NewReader(`{}`)).WithContext(ctx)
	response := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() { NewRouter().ServeHTTP(response, req); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("canceled request waited for the management lock")
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("HTTP %d: %s", response.Code, response.Body)
	}
}

func TestScheduleViewOnlyRecognizesOneExactHourlyPlan(t *testing.T) {
	for _, tc := range []struct {
		cron  string
		hours int
	}{
		{"0 */2 * * *", 2}, {"0 */2 * * 1", 0}, {"0 */2 * * * trailing", 0}, {"0 8 * * *", 0},
	} {
		m := Monitor{Schedules: []Schedule{{Cron: tc.cron}}, ScheduleHours: 5}
		setScheduleView(&m)
		if m.ScheduleHours != tc.hours || !m.ScheduleEnabled {
			t.Fatalf("%q: hours=%d", tc.cron, m.ScheduleHours)
		}
	}
	m := Monitor{Schedules: []Schedule{{Cron: "0 */2 * * *"}, {Cron: "0 */3 * * *"}}}
	setScheduleView(&m)
	if m.ScheduleHours != 0 {
		t.Fatal("multiple plans presented as one simple plan")
	}
}

func requestAPI(t *testing.T, router http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestStatusReportsActualFailedOrigin(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	for range 3 {
		DB.Create(&SwitchEvent{MonitorID: m.ID, Name: m.Name, FromIP: m.OriginalIP, ToIP: m.BackupIP, ToBackup: true, FromUnhealthy: true, Type: "auto", Timestamp: time.Now()})
		DB.Create(&SwitchEvent{MonitorID: m.ID, Name: m.Name, FromIP: m.BackupIP, ToIP: m.OriginalIP, Type: "auto", Timestamp: time.Now()})
	}
	response := requestAPI(t, NewRouter(), "GET", "/api/status", nil)
	if response.Code != 200 {
		t.Fatalf("status HTTP %d: %s", response.Code, response.Body)
	}
	var result struct {
		Hot []OfflineHotStat `json:"offline_hot"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Hot) != 1 || result.Hot[0].Count != 3 || result.Hot[0].IP != m.OriginalIP || result.Hot[0].Role != "primary" || result.Hot[0].LastAt.IsZero() {
		t.Fatalf("incorrect offline stats: %+v", result.Hot)
	}
}

func TestUpdateInvalidatesRecordIDWhenDomainChanges(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	response := requestAPI(t, NewRouter(), "PUT", "/api/monitors/1", map[string]any{
		"name": m.Name, "account_name": "test", "type": "http", "target": m.Target,
		"cf_zone_id": m.CFZoneID, "cf_domain": "new.example", "dns_type": "A",
		"original_ip": m.OriginalIP, "backup_ip": m.BackupIP,
	})
	if response.Code != 200 {
		t.Fatalf("HTTP %d: %s", response.Code, response.Body)
	}
	// Check the response under the update lock; a subsequent probe may resolve a new ID.
	var saved Monitor
	json.Unmarshal(response.Body.Bytes(), &saved)
	if saved.CFRecordID != "" || saved.CFDomain != "new.example" {
		t.Fatalf("stale record ID retained: %+v", saved)
	}
}

func TestScheduleEndpointAndEmptyScheduleRemoval(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	router := NewRouter()
	bad := requestAPI(t, router, "POST", "/api/monitors/1/schedule", map[string]any{"enabled": true, "cron": "not-cron", "target_ip": m.BackupIP})
	if bad.Code != 400 {
		t.Fatalf("invalid cron accepted: %d", bad.Code)
	}
	good := requestAPI(t, router, "POST", "/api/monitors/1/schedule", map[string]any{"enabled": true, "cron": "0 */2 * * *", "target_ip": m.BackupIP})
	if good.Code != 200 {
		t.Fatalf("schedule HTTP %d: %s", good.Code, good.Body)
	}
	var schedules []Schedule
	DB.Find(&schedules)
	if len(schedules) != 1 || schedules[0].Cron != "0 */2 * * *" {
		t.Fatal("schedule not saved")
	}
	removed := requestAPI(t, router, "PUT", "/api/monitors/1", map[string]any{
		"name": m.Name, "account_name": "test", "type": "http", "target": m.Target,
		"cf_zone_id": m.CFZoneID, "cf_domain": m.CFDomain, "dns_type": "A",
		"original_ip": m.OriginalIP, "backup_ip": m.BackupIP, "schedules": []any{},
	})
	if removed.Code != 200 {
		t.Fatalf("remove schedule HTTP %d: %s", removed.Code, removed.Body)
	}
	var count int64
	DB.Model(&Schedule{}).Count(&count)
	if count != 0 {
		t.Fatal("explicit empty schedules array did not clear schedules")
	}
}

func TestRejectsNonNumericMonitorID(t *testing.T) {
	testDatabase(t)
	router := NewRouter()
	for _, path := range []string{"/api/monitors/1%20OR%201=1", "/api/monitors/-1", "/api/monitors/0"} {
		response := requestAPI(t, router, "DELETE", path, nil)
		if response.Code != 400 {
			t.Fatalf("%s: HTTP %d", path, response.Code)
		}
	}
}

func TestConfigSaveFailureDoesNotChangeLiveCredentials(t *testing.T) {
	testDatabase(t)
	LoadedConfigPath = filepath.Join(t.TempDir(), "directory")
	if err := os.Mkdir(LoadedConfigPath, 0700); err != nil {
		t.Fatal(err)
	}
	err := UpdateConfig(func(config *Config) error { config.Accounts[0].ApiToken = "new-token"; return nil })
	if err == nil {
		t.Fatal("expected save failure")
	}
	if GetAccountConfig("test").ApiToken != "test-token" {
		t.Fatal("failed save changed live credentials")
	}
	if GetAccountConfig("missing") != nil {
		t.Fatal("unknown account silently fell back to another account")
	}
}

func TestConfigSnapshotsAreIndependentDuringConcurrentUpdates(t *testing.T) {
	testDatabase(t)
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			for range 10 {
				config := ConfigSnapshot()
				config.Accounts[0].ApiToken = "snapshot-only"
				if GetAccountConfig("test").ApiToken == "snapshot-only" {
					t.Error("snapshot aliased live configuration")
				}
			}
		})
	}
	for range 3 {
		if err := UpdateConfig(func(config *Config) error { config.Server.Debug = !config.Server.Debug; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	group.Wait()
}

func TestSeedRunsOnlyOnceAndPreservesExistingMonitors(t *testing.T) {
	testDatabase(t)
	AppConfig.Monitors = []MonitorConfig{{Name: "seed", Account: "test", Domain: "seed.example", ZoneID: "zone", Type: "ping", OriginalIP: "192.0.2.1", BackupIP: "192.0.2.2"}}
	if err := SeedMonitors(); err != nil {
		t.Fatal(err)
	}
	var m Monitor
	DB.First(&m)
	DB.Model(&m).Update("name", "edited in UI")
	if err := SeedMonitors(); err != nil {
		t.Fatal(err)
	}
	if got := loadTestMonitor(t, m.ID); got.Name != "edited in UI" {
		t.Fatal("restart overwrote UI changes")
	}
	DB.Delete(&m)
	if err := SeedMonitors(); err != nil {
		t.Fatal(err)
	}
	var count int64
	DB.Model(&Monitor{}).Count(&count)
	if count != 0 {
		t.Fatal("deleted monitor resurrected on restart")
	}
}

func TestAuthAndStaticRoutes(t *testing.T) {
	testDatabase(t)
	AppConfig.Server.AuthEnabled = true
	AppConfig.Server.JwtSecret = strings.Repeat("s", 32)
	router := NewRouter()
	unauthorized := requestAPI(t, router, "GET", "/api/config", nil)
	if unauthorized.Code != 401 {
		t.Fatal("credentials endpoint is not protected")
	}
	login := requestAPI(t, router, "POST", "/api/auth/login", map[string]string{"token": AppConfig.Server.JwtSecret})
	if login.Code != 200 || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login failed: %s", login.Body)
	}
	req := httptest.NewRequest("GET", "/api/config", nil)
	req.AddCookie(login.Result().Cookies()[0])
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != 200 {
		t.Fatalf("authenticated request failed: %s", response.Body)
	}
	for _, path := range []string{"/", "/login", "/app.js", "/js/api.js", "/healthz"} {
		if result := requestAPI(t, router, "GET", path, nil); result.Code != 200 {
			t.Errorf("%s: HTTP %d", path, result.Code)
		}
	}
}

func TestCloudflareCredentialsAreWriteOnlyAndBlankUpdatesPreserveThem(t *testing.T) {
	testDatabase(t)
	AppConfig.Accounts[0].Email = "test@example.com"
	AppConfig.Accounts[0].ApiKey = "private-api-key"
	router := NewRouter()
	for _, path := range []string{"/api/config", "/api/cloudflare-accounts"} {
		response := requestAPI(t, router, "GET", path, nil)
		body := response.Body.String()
		if response.Code != 200 || strings.Contains(body, "test-token") || strings.Contains(body, "private-api-key") ||
			path == "/api/cloudflare-accounts" && !strings.Contains(body, `"has_api_token":true`) {
			t.Fatalf("credential disclosure or missing status at %s: %s", path, body)
		}
	}
	response := requestAPI(t, router, "PUT", "/api/cloudflare-accounts/test", map[string]string{"name": "test", "api_token": "", "api_key": ""})
	if response.Code != 200 {
		t.Fatalf("blank credential update: %s", response.Body)
	}
	account := GetAccountConfig("test")
	if account.ApiToken != "test-token" || account.ApiKey != "private-api-key" {
		t.Fatal("blank update erased credentials")
	}
	response = requestAPI(t, router, "PUT", "/api/cloudflare-accounts/test", map[string]string{"name": "test", "api_token": "replacement"})
	if response.Code != 200 || GetAccountConfig("test").ApiToken != "replacement" {
		t.Fatal("credential rotation failed")
	}
}

func TestNotificationSettingsCannotChangeCloudflareCredentials(t *testing.T) {
	testDatabase(t)
	router := NewRouter()
	response := requestAPI(t, router, "POST", "/api/config", map[string]any{
		"cloudflare": map[string]string{"api_token": "wrong-account-token"},
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous credential update accepted: %s", response.Body)
	}
	response = requestAPI(t, router, "POST", "/api/config", NotificationConfig{})
	if response.Code != http.StatusOK || GetAccountConfig("test").ApiToken != "test-token" {
		t.Fatalf("notification save changed account credentials: %s", response.Body)
	}
}

func TestDNSManagementCannotBypassMonitorOwnership(t *testing.T) {
	testDatabase(t)
	m := createTestMonitor(t)
	DB.Model(&m).Update("cf_record_id", "")
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		fmt.Fprintf(w, `{"success":true,"result":{"id":"record","name":%q,"type":"A","content":%q}}`, m.CFDomain, m.OriginalIP)
	}))
	defer server.Close()
	mockCloudflare(200, "")
	cfClient.HTTP, cfClient.BaseURL = server.Client(), server.URL
	router := NewRouter()
	for _, method := range []string{"PUT", "DELETE", "POST"} {
		path := "/api/zones/zone/records"
		if method != "POST" {
			path += "/record"
		}
		response := requestAPI(t, router, method, path, map[string]any{"name": "WEBSITE.EXAMPLE.", "type": "A", "content": m.BackupIP})
		if response.Code != http.StatusConflict {
			t.Errorf("%s: HTTP %d: %s", method, response.Code, response.Body)
		}
	}
	for _, name := range []string{"@", "website"} {
		response := requestAPI(t, router, "POST", "/api/zones/zone/records", map[string]any{"name": name, "type": "A", "content": m.BackupIP})
		if response.Code != http.StatusBadRequest {
			t.Errorf("short DNS name accepted: %q", name)
		}
	}
	if writes.Load() != 0 {
		t.Fatalf("managed record received %d writes", writes.Load())
	}
}
