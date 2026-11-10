package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite"
)

var DB *gorm.DB

func InitDB() error {
	path := ConfigSnapshot().Database.Path
	if path == "" {
		path = "instance/cfguard.db"
	}
	db, err := openDatabase(path)
	if err == nil {
		DB = db
	}
	return err
}

// openDatabase owns the connection pool and closes it on every setup failure.
// Tests use the same driver, pragmas and schema as the application.
func openDatabase(path string) (*gorm.DB, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// A file URI escapes literal %, # and ? in paths instead of treating them as
	// connection options. Windows drive paths need the leading URI slash too.
	uri := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")}
	uri.RawQuery = url.Values{
		"_pragma":      {"busy_timeout(5000)", "journal_mode(WAL)", "foreign_keys(1)"},
		"_time_format": {"string_no_monotonic"},
	}.Encode()
	pool, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	// SQLite permits one writer. Bound connections and their page caches.
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	db, err := gorm.Open(sqlite.New(sqlite.Config{DriverName: "sqlite", Conn: pool}), &gorm.Config{
		Logger: logger.New(log.New(os.Stderr, "", log.LstdFlags), logger.Config{
			SlowThreshold: time.Second, LogLevel: logger.Error,
			IgnoreRecordNotFoundError: true, ParameterizedQueries: true,
		}),
	})
	if err == nil {
		err = db.AutoMigrate(&Monitor{}, &Schedule{}, &SwitchEvent{}, &Setting{})
	}
	if err != nil {
		pool.Close()
		return nil, err
	}
	return db, nil
}

type Setting struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

// Seed once, transactionally. Deleting the last monitor must not re-import it on restart.
func SeedMonitors() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var initialized Setting
		err := tx.First(&initialized, "key = ?", "monitors_seeded").Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var count int64
		if err := tx.Model(&Monitor{}).Count(&count).Error; err != nil {
			return err
		}
		// Existing installations keep all UI changes. YAML is only a bootstrap source.
		if count == 0 {
			for _, config := range ConfigSnapshot().Monitors {
				monitor := config.ToMonitor()
				for _, schedule := range config.Schedules {
					monitor.Schedules = append(monitor.Schedules, Schedule{Cron: schedule.Cron, TargetIP: schedule.TargetIP})
				}
				if err := prepareMonitor(&monitor); err != nil {
					return fmt.Errorf("seed monitor %q: %w", monitor.Name, err)
				}
				if err := checkDNSConflict(tx, &monitor); err != nil {
					return err
				}
				if err := tx.Create(&monitor).Error; err != nil {
					return err
				}
			}
		}
		return tx.Create(&Setting{Key: "monitors_seeded", Value: "1"}).Error
	})
}

func prepareMonitor(m *Monitor) error {
	if err := m.Validate(); err != nil {
		return err
	}
	account := GetAccountConfig(m.AccountName)
	if account == nil {
		return errors.New("configured Cloudflare account does not exist")
	}
	m.AccountName = account.Name
	m.PreferredIP = m.OriginalIP
	m.Status = "Unknown"
	return nil
}

func checkDNSConflict(tx *gorm.DB, m *Monitor) error {
	var count int64
	query := tx.Model(&Monitor{}).Where("id <> ? AND cf_zone_id = ?", m.ID, m.CFZoneID)
	if m.CFRecordID != "" {
		query = query.Where("(cf_record_id = ?) OR (cf_domain = ? AND dns_type = ?)", m.CFRecordID, m.CFDomain, m.DNSType)
	} else {
		query = query.Where("cf_domain = ? AND dns_type = ?", m.CFDomain, m.DNSType)
	}
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("another monitor already manages this DNS record")
	}
	return nil
}

var runtimeColumns = []string{
	"Status", "LastCheck", "FailCount", "SuccCount", "CurrentIP", "PreferredIP",
	"CFRecordID", "PrimaryHealthy", "BackupHealthy", "PrimaryError", "BackupError",
	"LastError", "LastSwitch",
}

func saveRuntime(tx *gorm.DB, m *Monitor) error {
	result := tx.Model(&Monitor{}).Where("id = ?", m.ID).Select(runtimeColumns).Updates(m)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func saveSwitch(db *gorm.DB, m *Monitor, from, kind string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := saveRuntime(tx, m); err != nil {
			return err
		}
		if kind == "manual" {
			if err := tx.Model(m).Update("original_ip_cdn_enabled", m.OriginalIPCDNEnabled).Error; err != nil {
				return err
			}
		}
		if sameAddress(from, m.CurrentIP) {
			return nil
		}
		var fromHealthy *bool
		if sameAddress(from, m.OriginalIP) {
			fromHealthy = m.PrimaryHealthy
		} else if sameAddress(from, m.BackupIP) {
			fromHealthy = m.BackupHealthy
		}
		return tx.Create(&SwitchEvent{
			MonitorID: m.ID, Name: m.Name, FromIP: from, ToIP: m.CurrentIP,
			ToBackup: m.CurrentIP == m.BackupIP, Type: kind, Timestamp: m.LastSwitch,
			FromUnhealthy: kind == "auto" && fromHealthy != nil && !*fromHealthy,
		}).Error
	})
}

func pruneHistory() {
	cutoff := time.Now().AddDate(0, 0, -ConfigSnapshot().Database.HistoryRetentionDays)
	if err := DB.Where("timestamp < ?", cutoff).Delete(&SwitchEvent{}).Error; err != nil {
		log.Printf("Prune switch history: %v", err)
	}
}
