package main

import "testing"

func TestMonitorValidation(t *testing.T) {
	valid := Monitor{Name: "site", CFDomain: "site.example", CFZoneID: "zone", OriginalIP: "192.0.2.1", BackupIP: "192.0.2.2", Type: "https", Target: "site.example/health", DNSType: "A"}
	for _, tc := range []struct {
		name   string
		change func(*Monitor)
	}{
		{"wrong IP family", func(m *Monitor) { m.BackupIP = "2001:db8::1" }},
		{"same origins", func(m *Monitor) { m.BackupIP = m.OriginalIP }},
		{"missing primary", func(m *Monitor) { m.OriginalIP = "" }},
		{"invalid timeout", func(m *Monitor) { m.Timeout = -1 }},
		{"invalid type", func(m *Monitor) { m.Type = "ftp" }},
		{"invalid target", func(m *Monitor) { m.Target = "https://" }},
		{"invalid TTL", func(m *Monitor) { m.DNSTTL = 10 }},
		{"IP as CNAME", func(m *Monitor) { m.DNSType = "CNAME"; m.BackupIP = "backup.example" }},
		{"invalid schedule", func(m *Monitor) { m.Schedules = []Schedule{{Cron: "invalid", TargetIP: m.BackupIP}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := valid
			tc.change(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("invalid monitor accepted")
			}
		})
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if valid.Target != "https://site.example/health" {
		t.Fatal("HTTPS type used the wrong default scheme")
	}
}

func TestScheduleNormalizesIPv6AndHostnames(t *testing.T) {
	for _, tc := range []struct{ target, primary, backup string }{
		{" 2001:0DB8:0:0:0:0:0:1 ", "2001:db8::1", "2001:db8::2"},
		{" PRIMARY.EXAMPLE. ", "primary.example", "backup.example"},
	} {
		schedule := Schedule{Cron: " 0 */2 * * * ", TargetIP: tc.target}
		if err := validateSchedule(&schedule, &Monitor{OriginalIP: tc.primary, BackupIP: tc.backup}); err != nil {
			t.Fatal(err)
		}
		if schedule.TargetIP != tc.primary || schedule.Cron != "0 */2 * * *" {
			t.Fatalf("not normalized: %+v", schedule)
		}
	}
}
