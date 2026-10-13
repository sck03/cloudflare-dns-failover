package main

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"cfguard/internal/health"
	"github.com/robfig/cron/v3"
)

// --- Models ---

type Schedule struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	MonitorID uint   `gorm:"index" json:"monitor_id"`
	Cron      string `json:"cron"`
	TargetIP  string `json:"target_ip"`
}

type Monitor struct {
	ID                   uint       `gorm:"primaryKey" json:"id"`
	Name                 string     `json:"name"`
	AccountName          string     `json:"account_name"`                                     // Refers to AppConfig.Accounts
	Target               string     `json:"target"`                                           // IP or Domain to check
	Type                 string     `json:"type"`                                             // ping, http
	DNSType              string     `gorm:"index:idx_monitor_dns,priority:3" json:"dns_type"` // A, AAAA, CNAME
	Interval             int        `json:"interval"`                                         // Seconds
	Timeout              int        `json:"timeout"`                                          // Seconds
	Retries              int        `json:"retries"`                                          // Failure threshold
	PingCount            int        `json:"ping_count"`
	SuccessThreshold     int        `json:"success_threshold"`
	DNSTTL               int        `json:"dns_ttl"`
	InsecureSkipVerify   bool       `json:"insecure_skip_verify"`
	ExpectedStatus       int        `json:"expected_status"`
	Status               string     `json:"status"` // Normal, Down
	LastCheck            time.Time  `json:"last_check"`
	FailCount            int        `json:"fail_count"`
	SuccCount            int        `json:"succ_count"`
	CurrentIP            string     `json:"current_ip"`
	PreferredIP          string     `json:"preferred_ip"`
	PrimaryHealthy       *bool      `json:"primary_healthy"`
	BackupHealthy        *bool      `json:"backup_healthy"`
	PrimaryError         string     `json:"primary_error"`
	BackupError          string     `json:"backup_error"`
	LastError            string     `json:"last_error"`
	LastSwitch           time.Time  `json:"last_switch"`
	OriginalIP           string     `json:"original_ip"`
	OriginalIPCDNEnabled bool       `json:"original_ip_cdn_enabled"` // CDN enabled for Original IP
	BackupIP             string     `json:"backup_ip"`
	BackupIPCDNEnabled   bool       `json:"backup_ip_cdn_enabled"` // CDN enabled for Backup IP
	CFZoneID             string     `gorm:"index:idx_monitor_dns,priority:1" json:"cf_zone_id"`
	CFRecordID           string     `json:"cf_record_id"`
	CFDomain             string     `gorm:"index:idx_monitor_dns,priority:2" json:"cf_domain"`
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
	DNSTTL               int              `yaml:"dns_ttl" json:"dns_ttl"`
	InsecureSkipVerify   bool             `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`
	ExpectedStatus       int              `yaml:"expected_status" json:"expected_status"`
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
		m.Type = "https"
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
		DNSTTL:               mc.DNSTTL,
		InsecureSkipVerify:   mc.InsecureSkipVerify,
		ExpectedStatus:       mc.ExpectedStatus,
		OriginalIP:           mc.OriginalIP,
		OriginalIPCDNEnabled: mc.OriginalIPCDNEnabled,
		BackupIP:             mc.BackupIP,
		BackupIPCDNEnabled:   mc.BackupIPCDNEnabled,
		CFZoneID:             mc.ZoneID,
		CFRecordID:           mc.RecordID,
		CFDomain:             mc.Domain,
	}

	return m
}

type ScheduleConfig struct {
	Cron     string `yaml:"cron" json:"cron"`
	TargetIP string `yaml:"target_ip" json:"target_ip"`
}

type SwitchEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	MonitorID uint      `gorm:"index" json:"monitor_id"`
	Name      string    `json:"name"`
	FromIP    string    `json:"from_ip"`
	ToIP      string    `json:"to_ip"`
	ToBackup  bool      `json:"to_backup"`
	Type      string    `json:"type"`
	Timestamp time.Time `gorm:"index" json:"timestamp"`
}

type OfflineHotStat struct {
	MonitorID uint      `json:"monitor_id"`
	Name      string    `json:"name"`
	IP        string    `json:"ip"`
	Role      string    `json:"role"`
	Count     int       `json:"count"`
	LastAt    time.Time `json:"last_at"`
}

func (m *Monitor) Validate() error {
	m.Name = strings.TrimSpace(m.Name)
	m.AccountName = strings.TrimSpace(m.AccountName)
	m.Type = strings.ToLower(strings.TrimSpace(m.Type))
	m.DNSType = strings.ToUpper(strings.TrimSpace(m.DNSType))
	m.CFDomain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(m.CFDomain), "."))
	m.CFZoneID = strings.ToLower(strings.TrimSpace(m.CFZoneID))
	m.CFRecordID = strings.ToLower(strings.TrimSpace(m.CFRecordID))
	m.Target = strings.TrimSpace(m.Target)
	m.OriginalIP = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(m.OriginalIP), "."))
	m.BackupIP = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(m.BackupIP), "."))
	if m.Name == "" || len(m.Name) > 200 || m.CFZoneID == "" || !validHostname(m.CFDomain) {
		return errors.New("name, zone ID and a valid DNS domain are required")
	}
	if m.Interval < 0 || m.Timeout < 0 || m.Retries < 0 || m.SuccessThreshold < 0 || m.PingCount < 0 {
		return errors.New("check intervals, timeouts and thresholds cannot be negative")
	}
	m.ApplyDefaults()
	if m.Interval > 86400 || m.Timeout > 120 || m.Retries > 100 || m.SuccessThreshold > 100 || m.PingCount > 10 {
		return errors.New("interval <= 86400, timeout <= 120, thresholds <= 100 and ping_count <= 10 are required")
	}
	for _, endpoint := range []*string{&m.OriginalIP, &m.BackupIP} {
		switch m.DNSType {
		case "A", "AAAA":
			ip, err := netip.ParseAddr(*endpoint)
			if err != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() ||
				(m.DNSType == "A" && !ip.Is4()) || (m.DNSType == "AAAA" && (!ip.Is6() || ip.Is4In6())) {
				return fmt.Errorf("%s requires valid %s origin addresses", *endpoint, m.DNSType)
			}
			*endpoint = ip.String()
		case "CNAME":
			if !validHostname(*endpoint) || *endpoint == m.CFDomain {
				return errors.New("CNAME origins must be independent hostnames, different from the managed DNS name")
			}
		default:
			return errors.New("dns_type must be A, AAAA or CNAME")
		}
	}
	if m.OriginalIP == m.BackupIP {
		return errors.New("primary and backup must be different addresses")
	}
	switch m.Type {
	case "ping":
		if m.Target == "" {
			m.Target = m.OriginalIP
		}
	case "http", "https":
		if m.Target == "" {
			return errors.New("HTTP/HTTPS checks require a target URL")
		}
		u, err := health.ParseTarget(m.Type, m.Target)
		if err != nil {
			return err
		}
		m.Target = u.String()
	default:
		return errors.New("type must be ping, http or https")
	}
	if m.ExpectedStatus != 0 && (m.ExpectedStatus < 200 || m.ExpectedStatus > 299) {
		return errors.New("expected_status must be 0 (any 2xx) or a status from 200 to 299")
	}
	if m.DNSTTL != 0 && m.DNSTTL != 1 && (m.DNSTTL < 60 || m.DNSTTL > 86400) {
		return errors.New("dns_ttl must be 0 (preserve), 1 (auto) or 60..86400 seconds")
	}
	for _, schedule := range m.Schedules {
		if err := validateSchedule(schedule, m); err != nil {
			return err
		}
	}
	return nil
}

func validHostname(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}

func validateSchedule(schedule Schedule, m *Monitor) error {
	if _, err := cron.ParseStandard(schedule.Cron); err != nil {
		return fmt.Errorf("invalid schedule: %w", err)
	}
	if schedule.TargetIP != m.OriginalIP && schedule.TargetIP != m.BackupIP {
		return errors.New("schedule target must equal primary or backup")
	}
	return nil
}

func (m *Monitor) healthSpec() health.Spec {
	return health.Spec{Type: m.Type, Target: m.Target, Timeout: time.Duration(m.Timeout) * time.Second,
		PingCount: m.PingCount, InsecureSkipVerify: m.InsecureSkipVerify, ExpectedStatus: m.ExpectedStatus}
}
