package main

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// --- Database ---

var DB *gorm.DB

func InitDB() {
	var err error
	dbPath := AppConfig.Database.Path
	if dbPath == "" {
		dbPath = "instance/cfguard.db"
	}
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatalf("Failed to create database directory: %v", err)
	}

	// Silent logger to reduce noise
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             time.Second,  // Slow SQL threshold
			LogLevel:                  logger.Error, // Log level (Silent, Error, Warn, Info)
			IgnoreRecordNotFoundError: true,         // Ignore ErrRecordNotFound error for logger
			ParameterizedQueries:      true,         // Don't include params in the SQL log
			Colorful:                  false,        // Disable color
		},
	)

	// Enable WAL mode for better concurrency and set busy timeout
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"

	DB, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Auto Migrate
	err = DB.AutoMigrate(&Monitor{}, &Schedule{})
	if err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}
}

func SeedMonitors() {
	if len(AppConfig.Monitors) == 0 {
		return
	}

	log.Println("Syncing monitors from config.yaml...")
	for _, mc := range AppConfig.Monitors {
		// Convert Config to Monitor Model (with defaults applied)
		configMonitor := mc.ToMonitor()

		// Check if monitor exists by name
		var existing Monitor
		result := DB.Where("name = ?", mc.Name).First(&existing)

		if result.Error == nil {
			// Found: Update Configurable Fields ONLY
			// We respect the Config file as the source of truth for configuration
			// Copy IDs to preserve them (though Updates() on struct ignores zero values if not carefully handled,
			// but we want to update even if zero? No, ToMonitor sets defaults.)

			// We only want to update configuration fields, not state fields.
			// configMonitor contains all config fields + defaults.
			// It has ID=0, Status="", etc.

			// Use explicit update to ensure we don't overwrite ID or State
			DB.Model(&existing).Updates(map[string]interface{}{
				"account_name":     configMonitor.AccountName,
				"target":           configMonitor.Target,
				"type":             configMonitor.Type,
				"dns_type":         configMonitor.DNSType,
				"interval":         configMonitor.Interval,
				"timeout":          configMonitor.Timeout,
				"retries":          configMonitor.Retries,
				"recovery_retries": configMonitor.RecoveryRetries,
				"original_ip":      configMonitor.OriginalIP,
				"backup_ip":        configMonitor.BackupIP,
				"cf_zone_id":       configMonitor.CFZoneID,
				"cf_record_id":     configMonitor.CFRecordID,
				"cf_domain":        configMonitor.CFDomain,
			})

			// Sync Schedules
			DB.Where("monitor_id = ?", existing.ID).Delete(&Schedule{})
			for _, sc := range mc.Schedules {
				s := Schedule{
					MonitorID: existing.ID,
					Cron:      sc.Cron,
					TargetIP:  sc.TargetIP,
				}
				DB.Create(&s)
			}

		} else {
			// Not Found: Create New
			// Set initial state
			configMonitor.Status = "Normal"
			configMonitor.LastCheck = time.Now()
			configMonitor.CurrentIP = configMonitor.OriginalIP

			DB.Create(&configMonitor)

			for _, sc := range mc.Schedules {
				s := Schedule{
					MonitorID: configMonitor.ID,
					Cron:      sc.Cron,
					TargetIP:  sc.TargetIP,
				}
				DB.Create(&s)
			}
			log.Printf("Created new monitor: %s", configMonitor.Name)
		}
	}
	log.Println("Monitor sync complete.")
}
