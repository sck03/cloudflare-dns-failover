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
	ID              uint       `gorm:"primaryKey" json:"id"`
	Name            string     `json:"name"`
	AccountName     string     `json:"account_name"`      // Refers to AppConfig.Accounts
	Target          string     `json:"target"`            // IP or Domain to check
	Type            string     `json:"type"`              // ping, http
	DNSType         string     `json:"dns_type"`          // A, AAAA, CNAME
	Interval        int        `json:"interval"`          // Seconds
	Timeout         int        `json:"timeout"`           // Seconds
	Retries         int        `json:"retries"`           // Failure threshold
	RecoveryRetries int        `json:"success_threshold"` // Recovery threshold
	Status          string     `json:"status"`            // Normal, Down
	LastCheck       time.Time  `json:"last_check"`
	FailCount       int        `json:"fail_count"`
	SuccCount       int        `json:"succ_count"`
	CurrentIP       string     `json:"current_ip"`
	BackupIP        string     `json:"backup_ip"`
	OriginalIP      string     `json:"original_ip"`
	CFZoneID        string     `json:"cf_zone_id"`
	CFRecordID      string     `json:"cf_record_id"`
	CFDomain        string     `json:"cf_domain"`
	Schedules       []Schedule `gorm:"foreignKey:MonitorID" json:"schedules"`
}

type MonitorConfig struct {
	Name            string           `yaml:"name" json:"name"`
	Account         string           `yaml:"account" json:"account_name"`
	Domain          string           `yaml:"domain" json:"cf_domain"`
	ZoneID          string           `yaml:"zone_id" json:"cf_zone_id"`
	RecordID        string           `yaml:"cf_record_id" json:"cf_record_id"`
	Type            string           `yaml:"type" json:"type"`
	DNSType         string           `yaml:"dns_type" json:"dns_type"`
	Target          string           `yaml:"target" json:"target"`
	OriginalIP      string           `yaml:"original_ip" json:"original_ip"`
	BackupIP        string           `yaml:"backup_ip" json:"backup_ip"`
	Interval        int              `yaml:"interval" json:"interval"`
	Timeout         int              `yaml:"timeout" json:"timeout"`
	Retries         int              `yaml:"retries" json:"retries"`
	RecoveryRetries int              `yaml:"recovery_retries" json:"success_threshold"`
	Schedules       []ScheduleConfig `yaml:"schedules" json:"schedules"`
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
	if m.RecoveryRetries <= 0 {
		m.RecoveryRetries = 2
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
		Name:            mc.Name,
		AccountName:     mc.Account,
		Target:          mc.Target,
		Type:            mc.Type,
		DNSType:         mc.DNSType,
		Interval:        mc.Interval,
		Timeout:         mc.Timeout,
		Retries:         mc.Retries,
		RecoveryRetries: mc.RecoveryRetries,
		OriginalIP:      mc.OriginalIP,
		BackupIP:        mc.BackupIP,
		CFZoneID:        mc.ZoneID,
		CFRecordID:      mc.RecordID,
		CFDomain:        mc.Domain,
	}

	m.ApplyDefaults()

	return m
}

type ScheduleConfig struct {
	Cron     string `yaml:"cron" json:"cron"`
	TargetIP string `yaml:"target_ip" json:"target_ip"`
}

type GlobalConfig struct {
	Key   string `gorm:"primaryKey" json:"key"`
	Value string `json:"value"`
}
