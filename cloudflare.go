package main

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"cfguard/internal/cloudflare"
)

var cfClient = cloudflare.NewClient()

type CloudflareZone = cloudflare.Zone
type CloudflareRecord = cloudflare.Record

func GetAccountConfig(name string) *AccountConfig {
	return accountFromConfig(ConfigSnapshot(), name)
}

func accountFromConfig(config Config, name string) *AccountConfig {
	if name == "" {
		name = config.ActiveAccount
		if name == "" && len(config.Accounts) > 0 {
			name = config.Accounts[0].Name
		}
	}
	for _, account := range config.Accounts {
		if account.Name == name {
			return &account
		}
	}
	return nil
}

func GetActiveAccountConfig() *AccountConfig { return GetAccountConfig("") }

func credentials(account *AccountConfig) cloudflare.Credentials {
	if account == nil {
		return cloudflare.Credentials{}
	}
	return cloudflare.Credentials{Token: account.ApiToken, Email: account.Email, Key: account.ApiKey}
}

// readDNSRecord resolves missing/stale IDs and refuses ambiguous multi-record names.
func readDNSRecord(ctx context.Context, m *Monitor) (*CloudflareRecord, error) {
	auth := credentials(GetAccountConfig(m.AccountName))
	if m.CFRecordID != "" {
		record, err := cfClient.Record(ctx, auth, m.CFZoneID, m.CFRecordID)
		if err == nil {
			if !matchesRecord(m, record) {
				return nil, errors.New("DNS record ID does not match the configured name/type")
			}
			return record, nil
		}
		var apiErr *cloudflare.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound && apiErr.Code != 81044 {
			return nil, err
		}
	}
	records, err := cfClient.Records(ctx, auth, m.CFZoneID, url.Values{"name": {m.CFDomain}, "type": {m.DNSType}})
	if err != nil {
		return nil, err
	}
	var match *CloudflareRecord
	for _, record := range records {
		if matchesRecord(m, &record) {
			if match != nil {
				return nil, errors.New("multiple matching DNS records; configure cf_record_id explicitly")
			}
			match = &record
		}
	}
	if match == nil {
		return nil, errors.New("matching DNS record not found")
	}
	m.CFRecordID = match.ID
	return match, nil
}

func matchesRecord(m *Monitor, record *CloudflareRecord) bool {
	return record.ID != "" && strings.EqualFold(record.Type, m.DNSType) &&
		strings.EqualFold(strings.TrimSuffix(record.Name, "."), strings.TrimSuffix(m.CFDomain, "."))
}

// updateDNS verifies record identity and the returned content/proxy settings.
// It returns the actual previous address without committing local state.
func updateDNS(ctx context.Context, m *Monitor, target string) (string, error) {
	if target != m.OriginalIP && target != m.BackupIP {
		return "", errors.New("switch target must be a configured primary or backup")
	}
	record, err := readDNSRecord(ctx, m)
	if err != nil {
		return "", err
	}
	if !sameAddress(record.Content, m.OriginalIP) && !sameAddress(record.Content, m.BackupIP) {
		return "", errors.New("DNS address is outside the configured primary/backup pair; reconcile the configuration first")
	}
	proxied := m.OriginalIPCDNEnabled
	if target == m.BackupIP {
		proxied = m.BackupIPCDNEnabled
	}
	ttl := m.DNSTTL
	if proxied {
		ttl = 1
	}
	from := record.Content
	if sameAddress(from, target) && record.Proxied == proxied && (ttl == 0 || record.TTL == ttl) {
		return from, nil
	}
	payload := map[string]any{"content": target, "proxied": proxied}
	if ttl > 0 {
		payload["ttl"] = ttl
	}
	updated, err := cfClient.SaveRecord(ctx, credentials(GetAccountConfig(m.AccountName)), m.CFZoneID, record.ID, payload)
	if err != nil {
		return "", err
	}
	if updated.ID != record.ID || !matchesRecord(m, updated) || !sameAddress(updated.Content, target) || updated.Proxied != proxied || ttl > 0 && updated.TTL != ttl {
		return "", errors.New("Cloudflare did not confirm the requested DNS content/proxy settings")
	}
	return from, nil
}

func sameAddress(a, b string) bool {
	if first, err := netip.ParseAddr(a); err == nil {
		second, err := netip.ParseAddr(b)
		return err == nil && first == second
	}
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}

// ConfigureTTL lowers the live record's TTL before an outage. Patch only TTL,
// preserving the provider's current address, proxy mode and other record metadata.
func (cloudflareDNS) ConfigureTTL(ctx context.Context, m *Monitor, record *CloudflareRecord) error {
	ttl := m.DNSTTL
	if ttl == 0 {
		return nil
	}
	if record.Proxied {
		ttl = 1
	}
	if record.TTL == ttl {
		return nil
	}
	updated, err := cfClient.SaveRecord(ctx, credentials(GetAccountConfig(m.AccountName)), m.CFZoneID, record.ID, map[string]any{"ttl": ttl})
	if err != nil {
		return err
	}
	if !matchesRecord(m, updated) || updated.ID != record.ID || !sameAddress(updated.Content, record.Content) || updated.Proxied != record.Proxied || updated.TTL != ttl {
		return errors.New("Cloudflare did not confirm the TTL update; reconciliation required")
	}
	return nil
}
