package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

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
		DB.Create(&SwitchEvent{MonitorID: m.ID, Name: m.Name, FromIP: m.OriginalIP, ToIP: m.BackupIP, ToBackup: true, Type: "auto", Timestamp: time.Now()})
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
	if len(result.Hot) != 1 || result.Hot[0].IP != m.OriginalIP || result.Hot[0].Role != "primary" || result.Hot[0].LastAt.IsZero() {
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
