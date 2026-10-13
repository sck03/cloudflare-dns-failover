package main

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Configuration mutations are short and infrequent. Serialize them to keep
// account references and DNS ownership checks consistent across API requests.
var managementMutex sync.Mutex

func apiError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func monitorID(w http.ResponseWriter, r *http.Request) (uint, bool) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 32)
	if err != nil || id == 0 {
		apiError(w, http.StatusBadRequest, errors.New("invalid monitor ID"))
		return 0, false
	}
	return uint(id), true
}

func GetMonitors(w http.ResponseWriter, r *http.Request) {
	monitors := make([]Monitor, 0)
	if err := DB.Preload("Schedules").Order("id").Find(&monitors).Error; err != nil {
		apiError(w, 500, err)
		return
	}
	for i := range monitors {
		setScheduleView(&monitors[i])
	}
	writeJSON(w, http.StatusOK, monitors)
}

func setScheduleView(m *Monitor) {
	m.ScheduleEnabled = len(m.Schedules) > 0
	if m.ScheduleEnabled {
		fmt.Sscanf(m.Schedules[0].Cron, "0 */%d * * *", &m.ScheduleHours)
	}
}

func GetStatus(w http.ResponseWriter, r *http.Request) {
	monitors := make([]Monitor, 0)
	history := make([]SwitchEvent, 0)
	if err := DB.Preload("Schedules").Order("id").Find(&monitors).Error; err != nil {
		apiError(w, 500, err)
		return
	}
	for i := range monitors {
		setScheduleView(&monitors[i])
	}
	if err := DB.Order("timestamp desc").Limit(50).Find(&history).Error; err != nil {
		apiError(w, 500, err)
		return
	}
	// Aggregate in SQLite and report the endpoint that failed, not its replacement.
	hot := make([]OfflineHotStat, 0)
	totals := DB.Model(&SwitchEvent{}).
		Select("MAX(id) AS last_id, COUNT(*) AS total_count").
		Where("type = ? AND timestamp > ?", "auto", time.Now().Add(-24*time.Hour)).
		Group("monitor_id, from_ip, to_backup").Having("COUNT(*) >= 3")
	if err := DB.Table("(?) AS totals", totals).Joins("JOIN switch_events AS e ON e.id = totals.last_id").
		Select("e.monitor_id, e.name, e.from_ip AS ip, CASE WHEN e.to_backup THEN 'primary' ELSE 'backup' END AS role, totals.total_count AS count, e.timestamp AS last_at").
		Order("totals.total_count DESC").Limit(10).Scan(&hot).Error; err != nil {
		apiError(w, 500, err)
		return
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	writeJSON(w, http.StatusOK, map[string]any{
		"system":   map[string]any{"goroutines": runtime.NumGoroutine(), "mem_alloc": memory.Alloc},
		"monitors": monitors, "history": history, "offline_hot": hot,
	})
}

func CreateMonitor(w http.ResponseWriter, r *http.Request) {
	var input MonitorConfig
	if err := decodeJSON(r, &input); err != nil {
		apiError(w, 400, err)
		return
	}
	managementMutex.Lock()
	defer managementMutex.Unlock()
	monitor := input.ToMonitor()
	monitor.Schedules = schedulesFromConfig(input.Schedules)
	if err := prepareMonitor(&monitor); err != nil {
		apiError(w, 400, err)
		return
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := checkDNSConflict(tx, &monitor); err != nil {
			return err
		}
		return tx.Create(&monitor).Error
	})
	if err != nil {
		apiError(w, 409, err)
		return
	}
	if err := engine.Reload(); err != nil {
		apiError(w, 500, fmt.Errorf("monitor saved but scheduler reload failed: %w", err))
		return
	}
	writeJSON(w, http.StatusCreated, monitor)
}

func schedulesFromConfig(configs []ScheduleConfig) []Schedule {
	schedules := make([]Schedule, 0, len(configs))
	for _, config := range configs {
		schedules = append(schedules, Schedule{Cron: config.Cron, TargetIP: strings.ToLower(strings.TrimSuffix(strings.TrimSpace(config.TargetIP), "."))})
	}
	return schedules
}

var configColumns = []string{
	"Name", "AccountName", "Target", "Type", "DNSType", "Interval", "Timeout", "Retries",
	"PingCount", "SuccessThreshold", "DNSTTL", "InsecureSkipVerify", "ExpectedStatus",
	"OriginalIP", "OriginalIPCDNEnabled", "BackupIP", "BackupIPCDNEnabled", "CFZoneID", "CFRecordID", "CFDomain",
}

func UpdateMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := monitorID(w, r)
	if !ok {
		return
	}
	var input MonitorConfig
	if err := decodeJSON(r, &input); err != nil {
		apiError(w, 400, err)
		return
	}
	managementMutex.Lock()
	defer managementMutex.Unlock()
	unlock, err := engine.Lock(r.Context(), id)
	if err != nil {
		apiError(w, 503, err)
		return
	}
	defer unlock()
	var previous Monitor
	if err := DB.Preload("Schedules").First(&previous, id).Error; err != nil {
		apiError(w, 404, err)
		return
	}
	next := input.ToMonitor()
	next.ID = id
	next.Schedules = previous.Schedules
	if input.Schedules != nil {
		next.Schedules = schedulesFromConfig(input.Schedules)
	}
	if err := next.Validate(); err != nil {
		apiError(w, 400, err)
		return
	}
	account := GetAccountConfig(next.AccountName)
	if account == nil {
		apiError(w, 400, errors.New("configured Cloudflare account does not exist"))
		return
	}
	next.AccountName = account.Name
	identityChanged := next.AccountName != previous.AccountName || next.CFZoneID != previous.CFZoneID ||
		next.CFDomain != previous.CFDomain || next.DNSType != previous.DNSType
	if next.CFRecordID == "" && !identityChanged {
		next.CFRecordID = previous.CFRecordID
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := checkDNSConflict(tx, &next); err != nil {
			return err
		}
		if err := tx.Model(&Monitor{}).Where("id = ?", id).Select(configColumns).Updates(&next).Error; err != nil {
			return err
		}
		reset := map[string]any{
			"status": "Unknown", "fail_count": 0, "succ_count": 0, "last_error": "",
			"primary_healthy": nil, "backup_healthy": nil, "primary_error": "", "backup_error": "",
			"last_check": time.Time{},
		}
		if identityChanged {
			reset["current_ip"] = ""
		}
		if identityChanged || previous.PreferredIP != next.OriginalIP && previous.PreferredIP != next.BackupIP {
			reset["preferred_ip"] = next.OriginalIP
		}
		if err := tx.Model(&Monitor{}).Where("id = ?", id).Updates(reset).Error; err != nil {
			return err
		}
		if input.Schedules != nil {
			return replaceSchedules(tx, id, next.Schedules)
		}
		return nil
	})
	if err != nil {
		apiError(w, 409, err)
		return
	}
	engine.Invalidate(id)
	if err := engine.Reload(); err != nil {
		apiError(w, 500, err)
		return
	}
	if err := DB.Preload("Schedules").First(&next, id).Error; err != nil {
		apiError(w, 500, err)
		return
	}
	setScheduleView(&next)
	writeJSON(w, http.StatusOK, next)
}

func replaceSchedules(tx *gorm.DB, id uint, schedules []Schedule) error {
	if err := tx.Where("monitor_id = ?", id).Delete(&Schedule{}).Error; err != nil {
		return err
	}
	for _, schedule := range schedules {
		schedule.ID, schedule.MonitorID = 0, id
		if err := tx.Create(&schedule).Error; err != nil {
			return err
		}
	}
	return nil
}

func SaveSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := monitorID(w, r)
	if !ok {
		return
	}
	var input struct {
		Enabled  bool   `json:"enabled"`
		Cron     string `json:"cron"`
		TargetIP string `json:"target_ip"`
	}
	if err := decodeJSON(r, &input); err != nil {
		apiError(w, 400, err)
		return
	}
	managementMutex.Lock()
	defer managementMutex.Unlock()
	unlock, err := engine.Lock(r.Context(), id)
	if err != nil {
		apiError(w, 503, err)
		return
	}
	defer unlock()
	var monitor Monitor
	if err := DB.First(&monitor, id).Error; err != nil {
		apiError(w, 404, err)
		return
	}
	schedules := make([]Schedule, 0, 1)
	if input.Enabled {
		schedule := Schedule{Cron: input.Cron, TargetIP: input.TargetIP}
		if err := validateSchedule(schedule, &monitor); err != nil {
			apiError(w, 400, err)
			return
		}
		schedules = append(schedules, schedule)
	}
	if err := DB.Transaction(func(tx *gorm.DB) error { return replaceSchedules(tx, id, schedules) }); err != nil {
		apiError(w, 500, err)
		return
	}
	if err := engine.Reload(); err != nil {
		apiError(w, 500, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "saved"})
}

func RestoreMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := monitorID(w, r)
	if !ok {
		return
	}
	var input struct {
		Proxied *bool `json:"proxied"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &input); err != nil {
			apiError(w, 400, err)
			return
		}
	}
	monitor, err := engine.Switch(r.Context(), id, "", "manual", input.Proxied)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		apiError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, monitor)
}

func DeleteMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := monitorID(w, r)
	if !ok {
		return
	}
	managementMutex.Lock()
	defer managementMutex.Unlock()
	unlock, err := engine.Lock(r.Context(), id)
	if err != nil {
		apiError(w, 503, err)
		return
	}
	defer unlock()
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("monitor_id = ?", id).Delete(&Schedule{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&Monitor{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		apiError(w, 404, err)
		return
	}
	if err := engine.Reload(); err != nil {
		apiError(w, 500, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "deleted"})
}
