package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cfguard/internal/cloudflare"
	"cfguard/internal/health"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testDatabase(t *testing.T) {
	t.Helper()
	oldDB, oldConfig, oldClient := DB, AppConfig, cfClient
	oldEngine, oldPath := engine, LoadedConfigPath
	directory := t.TempDir()
	var err error
	DB, err = gorm.Open(sqlite.Open(filepath.Join(directory, "test.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := DB.DB()
	sqlDB.SetMaxOpenConns(1)
	if err = DB.AutoMigrate(&Monitor{}, &Schedule{}, &SwitchEvent{}, &Setting{}); err != nil {
		t.Fatal(err)
	}
	AppConfig = Config{Accounts: []AccountConfig{{Name: "test", ApiToken: "test-token"}}}
	if err := validateConfig(&AppConfig); err != nil {
		t.Fatal(err)
	}
	LoadedConfigPath = filepath.Join(directory, "config.yaml")
	engine = NewEngine(DB, healthyChecker{}, stubDNS{}, 2, func(string) {})
	t.Cleanup(func() {
		engine.Stop()
		db, _ := DB.DB()
		db.Close()
		DB, AppConfig, cfClient = oldDB, oldConfig, oldClient
		engine, LoadedConfigPath = oldEngine, oldPath
	})
}

type healthyChecker struct{}

func (healthyChecker) Check(context.Context, health.Spec, string) error { return nil }

type stubDNS struct{}

func (stubDNS) Read(_ context.Context, m *Monitor) (*CloudflareRecord, error) {
	address := m.CurrentIP
	if address == "" {
		address = m.OriginalIP
	}
	return &CloudflareRecord{ID: "record", Name: m.CFDomain, Type: m.DNSType, Content: address}, nil
}

func (stubDNS) Switch(_ context.Context, m *Monitor, _ string) (string, error) {
	return m.CurrentIP, nil
}

func mockCloudflare(status int, body string) {
	cfClient = cloudflare.NewClient()
	cfClient.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}

func TestDNSUpdateRejectsUnsuccessfulEnvelope(t *testing.T) {
	testDatabase(t)
	mockCloudflare(http.StatusOK, `{"success":false,"errors":[{"code":1000,"message":"update rejected"}]}`)
	m := Monitor{AccountName: "test", DNSType: "A", CFZoneID: "zone", CFRecordID: "record", CFDomain: "example.com", OriginalIP: "192.0.2.1", BackupIP: "192.0.2.2"}
	if _, err := updateDNS(context.Background(), &m, m.BackupIP); err == nil {
		t.Fatal("an unsuccessful Cloudflare response must not confirm a DNS switch")
	}
}

func TestManualRestorePreservesStateOnDNSFailure(t *testing.T) {
	testDatabase(t)
	mockCloudflare(http.StatusForbidden, `{"success":false,"errors":[{"message":"permission denied"}]}`)
	engine.dns = cloudflareDNS{}
	m := Monitor{Name: "website", AccountName: "test", Type: "ping", DNSType: "A", CFZoneID: "zone", CFRecordID: "record", CFDomain: "example.com", Status: "Down", CurrentIP: "192.0.2.2", OriginalIP: "192.0.2.1", BackupIP: "192.0.2.2"}
	if err := DB.Create(&m).Error; err != nil {
		t.Fatal(err)
	}
	router := http.NewServeMux()
	router.HandleFunc("POST /api/monitors/{id}/restore", RestoreMonitor)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/monitors/1/restore", strings.NewReader(`{}`)))
	var saved Monitor
	if err := DB.First(&saved, m.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.CurrentIP != m.CurrentIP || saved.Status != m.Status {
		t.Errorf("failed DNS update changed local state: got %s/%s, want %s/%s", saved.CurrentIP, saved.Status, m.CurrentIP, m.Status)
	}
	if response.Code < 400 {
		t.Errorf("failed DNS update returned HTTP %d", response.Code)
	}
}
