package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestDatabasePathPragmasAndTimes(t *testing.T) {
	name := "数据库 #100% +&.db"
	if runtime.GOOS != "windows" {
		name += "?literal"
	}
	path := filepath.Join(t.TempDir(), name)
	db, err := openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database was not created at its literal path: %v", err)
	}
	for pragma, want := range map[string]string{"journal_mode": "wal", "foreign_keys": "1", "busy_timeout": "5000"} {
		var got string
		if err := pool.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %q, want %q: %v", pragma, got, want, err)
		}
	}
	stamp := time.Now()
	event := SwitchEvent{Timestamp: stamp}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	var saved SwitchEvent
	if err := db.First(&saved, event.ID).Error; err != nil || !saved.Timestamp.Equal(stamp) {
		t.Fatalf("timestamp did not round trip: %v, %v", saved.Timestamp, err)
	}
	var count int64
	if err := db.Model(&SwitchEvent{}).Where("timestamp = ?", saved.Timestamp).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("timestamp cannot be queried after scanning: count=%d, err=%v", count, err)
	}
	if err := db.Create(&Schedule{MonitorID: 999, Cron: "0 0 * * *"}).Error; err == nil {
		t.Fatal("foreign key constraint was not enforced")
	}
}

func TestDatabaseBusyWaitHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locked.db")
	first, err := openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := openDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, db := range []*gorm.DB{first, second} {
		pool, _ := db.DB()
		t.Cleanup(func() { pool.Close() })
	}
	tx := first.Begin()
	defer tx.Rollback()
	if err := tx.Create(&Setting{Key: "held", Value: "1"}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	pool, _ := second.DB()
	_, err = pool.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES (?, ?)", "blocked", "2")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected canceled lock wait, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("query waited for the 5-second busy timeout instead of its context")
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	if err := second.Create(&Setting{Key: "after", Value: "3"}).Error; err != nil {
		t.Fatalf("connection unusable after cancellation: %v", err)
	}
}
