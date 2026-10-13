package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitDB() error {
	path := ConfigSnapshot().Database.Path
	if path == "" {
		path = "instance/cfguard.db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	var err error
	DB, err = gorm.Open(sqlite.Open(path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"), &gorm.Config{
		Logger: logger.New(log.New(os.Stderr, "", log.LstdFlags), logger.Config{
			SlowThreshold: time.Second, LogLevel: logger.Error,
			IgnoreRecordNotFoundError: true, ParameterizedQueries: true,
		}),
	})
	if err != nil {
		return err
	}
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	// SQLite permits one writer. Bound connections instead of growing a pool
	// whose concurrent writes contend for the same database file.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	return DB.AutoMigrate(&Monitor{}, &Schedule{}, &SwitchEvent{}, &Setting{})
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
		if account := GetActiveAccountConfig(); account != nil {
			if err := tx.Model(&Monitor{}).Where("account_name = ''").Update("account_name", account.Name).Error; err != nil {
				return err
			}
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
		return tx.Create(&SwitchEvent{
			MonitorID: m.ID, Name: m.Name, FromIP: from, ToIP: m.CurrentIP,
			ToBackup: m.CurrentIP == m.BackupIP, Type: kind, Timestamp: m.LastSwitch,
		}).Error
	})
}

func pruneHistory() {
	cutoff := time.Now().AddDate(0, 0, -ConfigSnapshot().Database.HistoryRetentionDays)
	if err := DB.Where("timestamp < ?", cutoff).Delete(&SwitchEvent{}).Error; err != nil {
		log.Printf("Prune switch history: %v", err)
	}
}
