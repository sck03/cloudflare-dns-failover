package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// --- Cloudflare Service ---

var cfClient = &http.Client{
	Timeout: 15 * time.Second,
}

func GetAccountConfig(name string) *AccountConfig {
	for i := range AppConfig.Accounts {
		if AppConfig.Accounts[i].Name == name {
			return &AppConfig.Accounts[i]
		}
	}
	// Fallback to first if not found or empty
	if len(AppConfig.Accounts) > 0 {
		return &AppConfig.Accounts[0]
	}
	return nil
}

func GetActiveAccountConfig() *AccountConfig {
	if AppConfig.ActiveAccount != "" {
		if acc := GetAccountConfig(AppConfig.ActiveAccount); acc != nil {
			return acc
		}
	}
	return GetAccountConfig("")
}

func newCloudflareRequest(method, url string, body io.Reader, acc *AccountConfig) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	if acc.ApiToken != "" {
		req.Header.Set("Authorization", "Bearer "+acc.ApiToken)
	} else {
		req.Header.Set("X-Auth-Email", acc.Email)
		req.Header.Set("X-Auth-Key", acc.ApiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

func UpdateCloudflareDNS(m *Monitor, targetIP string) bool {
	if m.CFZoneID == "" || targetIP == "" {
		log.Println("Skipping DNS update: Missing ZoneID or TargetIP")
		return false
	}

	if m.CFRecordID == "" {
		log.Println("RecordID missing, attempting to fetch...")
		newID, err := FetchCloudflareRecordID(m)
		if err == nil && newID != "" {
			m.CFRecordID = newID
			// Save to DB for future use
			if err := DB.Model(m).Update("cf_record_id", newID).Error; err != nil {
				log.Printf("Failed to save new RecordID to DB: %v", err)
			}
			log.Printf("Fetched and saved new Record ID: %s", newID)
		} else {
			log.Printf("Failed to fetch Record ID: %v, aborting update.", err)
			return false
		}
	}

	acc := GetAccountConfig(m.AccountName)
	if acc == nil {
		log.Println("No Cloudflare account configured")
		return false
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", m.CFZoneID, m.CFRecordID)

	// Construct payload
	dnsType := m.DNSType
	if dnsType == "" {
		dnsType = "A"
	}

	payload := map[string]interface{}{
		"content": targetIP,
		"name":    m.CFDomain,
		"type":    dnsType,
	}

	// Determine proxy status based on target IP
	if targetIP == m.OriginalIP {
		payload["proxied"] = m.OriginalIPProxy
	} else if targetIP == m.BackupIP {
		payload["proxied"] = m.BackupIPProxy
	}

	jsonPayload, _ := json.Marshal(payload)

	req, err := newCloudflareRequest("PATCH", url, bytes.NewBuffer(jsonPayload), acc)
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		return false
	}

	resp, err := cfClient.Do(req)
	if err != nil {
		log.Printf("Failed to update DNS: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("Successfully updated DNS for %s to %s", m.Name, targetIP)
		return true
	} else {
		// Read body for error details
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Failed to update DNS, status: %d, body: %s", resp.StatusCode, string(body))
		return false
	}
}

func FetchCloudflareRecordID(m *Monitor) (string, error) {
	accConfig := GetAccountConfig(m.AccountName)
	if accConfig == nil {
		return "", fmt.Errorf("account config not found for %s", m.AccountName)
	}

	dnsType := m.DNSType
	if dnsType == "" {
		dnsType = "A"
	}

	// Create request to list records
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?name=%s&type=%s", m.CFZoneID, m.CFDomain, dnsType)

	req, err := newCloudflareRequest("GET", url, nil, accConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := cfClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch CF Record ID: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Result []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %v, body: %s", err, string(body))
	}

	if !result.Success {
		errMsg := "unknown error"
		if len(result.Errors) > 0 {
			errMsg = result.Errors[0].Message
		}
		return "", fmt.Errorf("cloudflare api error: %s", errMsg)
	}

	if len(result.Result) > 0 {
		// Try to find a record that matches the OriginalIP (Primary)
		if m.OriginalIP != "" {
			for _, record := range result.Result {
				if record.Content == m.OriginalIP {
					return record.ID, nil
				}
			}
		}
		// If no match found (or OriginalIP is empty), return the first one
		return result.Result[0].ID, nil
	}
	return "", fmt.Errorf("record not found")
}

type CloudflareZone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Paused bool   `json:"paused"`
}

type CloudflareRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

func FetchCloudflareZones(acc *AccountConfig) ([]CloudflareZone, error) {
	if acc == nil {
		return []CloudflareZone{}, nil
	}
	url := "https://api.cloudflare.com/client/v4/zones?per_page=100"
	req, err := newCloudflareRequest("GET", url, nil, acc)
	if err != nil {
		return nil, err
	}
	resp, err := cfClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Result []CloudflareZone `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if !result.Success {
		errMsg := "unknown error"
		if len(result.Errors) > 0 {
			errMsg = result.Errors[0].Message
		}
		return nil, fmt.Errorf("cloudflare api error: %s", errMsg)
	}
	return result.Result, nil
}

func FetchCloudflareRecords(acc *AccountConfig, zoneID string) ([]CloudflareRecord, error) {
	if acc == nil || zoneID == "" {
		return []CloudflareRecord{}, nil
	}
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?per_page=100", zoneID)
	req, err := newCloudflareRequest("GET", url, nil, acc)
	if err != nil {
		return nil, err
	}
	resp, err := cfClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Result []CloudflareRecord `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if !result.Success {
		errMsg := "unknown error"
		if len(result.Errors) > 0 {
			errMsg = result.Errors[0].Message
		}
		return nil, fmt.Errorf("cloudflare api error: %s", errMsg)
	}
	return result.Result, nil
}
