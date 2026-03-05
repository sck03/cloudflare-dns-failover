package main

import (
	"time"
)

// --- Models ---

type Schedule struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	MonitorID uint   `json:"monitor_id"`
	Cron      string `json:"cron"`
	TargetIP  string `json:"target_ip"`
}

type Monitor struct {
	ID                   uint       `gorm:"primaryKey" json:"id"`
	Name                 string     `json:"name"`
	AccountName          string     `json:"account_name"` // Refers to AppConfig.Accounts
	Target               string     `json:"target"`       // IP or Domain to check
	Type                 string     `json:"type"`         // ping, http
	DNSType              string     `json:"dns_type"`     // A, AAAA, CNAME
	Interval             int        `json:"interval"`     // Seconds
	Timeout              int        `json:"timeout"`      // Seconds
	Retries              int        `json:"retries"`      // Failure threshold
	PingCount            int        `json:"ping_count"`
	SuccessThreshold     int        `json:"success_threshold"`
	Status               string     `json:"status"` // Normal, Down
	LastCheck            time.Time  `json:"last_check"`
	FailCount            int        `json:"fail_count"`
	SuccCount            int        `json:"succ_count"`
	CurrentIP            string     `json:"current_ip"`
	OriginalIP           string     `json:"original_ip"`
	OriginalIPCDNEnabled bool       `json:"original_ip_cdn_enabled"` // CDN enabled for Original IP
	BackupIP             string     `json:"backup_ip"`
	BackupIPCDNEnabled   bool       `json:"backup_ip_cdn_enabled"` // CDN enabled for Backup IP
	CFZoneID             string     `json:"cf_zone_id"`
	CFRecordID           string     `json:"cf_record_id"`
	CFDomain             string     `json:"cf_domain"`
	Schedules            []Schedule `gorm:"foreignKey:MonitorID" json:"schedules"`
	ScheduleEnabled      bool       `gorm:"-" json:"schedule_enabled"`
	ScheduleHours        int        `gorm:"-" json:"schedule_hours"`
}

type MonitorConfig struct {
	Name                 string           `yaml:"name" json:"name"`
	Account              string           `yaml:"account" json:"account_name"`
	Domain               string           `yaml:"domain" json:"cf_domain"`
	ZoneID               string           `yaml:"zone_id" json:"cf_zone_id"`
	RecordID             string           `yaml:"cf_record_id" json:"cf_record_id"`
	Type                 string           `yaml:"type" json:"type"`
	DNSType              string           `yaml:"dns_type" json:"dns_type"`
	Target               string           `yaml:"target" json:"target"`
	OriginalIP           string           `yaml:"original_ip" json:"original_ip"`
	OriginalIPCDNEnabled bool             `yaml:"original_ip_cdn_enabled" json:"original_ip_cdn_enabled"`
	BackupIP             string           `yaml:"backup_ip" json:"backup_ip"`
	BackupIPCDNEnabled   bool             `yaml:"backup_ip_cdn_enabled" json:"backup_ip_cdn_enabled"`
	Interval             int              `yaml:"interval" json:"interval"`
	Timeout              int              `yaml:"timeout" json:"timeout"`
	Retries              int              `yaml:"retries" json:"retries"`
	PingCount            int              `yaml:"ping_count" json:"ping_count"`
	SuccessThreshold     int              `yaml:"success_threshold" json:"success_threshold"`
	Schedules            []ScheduleConfig `yaml:"schedules" json:"schedules"`
}

func (m *Monitor) ApplyDefaults() {
	if m.Interval <= 0 {
		m.Interval = 60
	}
	if m.Timeout <= 0 {
		m.Timeout = 5
	}
	if m.Retries <= 0 {
		m.Retries = 3
	}
	if m.PingCount <= 0 {
		m.PingCount = 3
	}
	if m.SuccessThreshold <= 0 {
		m.SuccessThreshold = 2
	}
	if m.Type == "" {
		m.Type = "ping"
	}
	if m.DNSType == "" {
		m.DNSType = "A"
	}
}

func (mc *MonitorConfig) ToMonitor() Monitor {
	m := Monitor{
		Name:                 mc.Name,
		AccountName:          mc.Account,
		Target:               mc.Target,
		Type:                 mc.Type,
		DNSType:              mc.DNSType,
		Interval:             mc.Interval,
		Timeout:              mc.Timeout,
		Retries:              mc.Retries,
		PingCount:            mc.PingCount,
		SuccessThreshold:     mc.SuccessThreshold,
		OriginalIP:           mc.OriginalIP,
		OriginalIPCDNEnabled: mc.OriginalIPCDNEnabled,
		BackupIP:             mc.BackupIP,
		BackupIPCDNEnabled:   mc.BackupIPCDNEnabled,
		CFZoneID:             mc.ZoneID,
		CFRecordID:           mc.RecordID,
		CFDomain:             mc.Domain,
	}

	m.ApplyDefaults()

	return m
}

type ScheduleConfig struct {
	Cron     string `yaml:"cron" json:"cron"`
	TargetIP string `yaml:"target_ip" json:"target_ip"`
}

type SwitchEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	MonitorID uint      `json:"monitor_id"`
	Name      string    `json:"name"`
	FromIP    string    `json:"from_ip"`
	ToIP      string    `json:"to_ip"`
	ToBackup  bool      `json:"to_backup"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
}

type OfflineHotStat struct {
	MonitorID uint      `json:"monitor_id"`
	Name      string    `json:"name"`
	IP        string    `json:"ip"`
	Role      string    `json:"role"`
	Count     int       `json:"count"`
	LastAt    time.Time `json:"last_at"`
}
