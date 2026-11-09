package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

func GetZones(w http.ResponseWriter, r *http.Request) {
	account := GetActiveAccountConfig()
	if account == nil {
		writeJSON(w, http.StatusOK, []CloudflareZone{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	zones, err := cfClient.Zones(ctx, credentials(account))
	if err != nil {
		apiError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, zones)
}

func GetZoneRecords(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	records, err := cfClient.Records(ctx, credentials(GetActiveAccountConfig()), r.PathValue("zoneId"), nil)
	if err != nil {
		apiError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func CreateRecord(w http.ResponseWriter, r *http.Request) { saveRecord(w, r, "") }
func UpdateRecord(w http.ResponseWriter, r *http.Request) { saveRecord(w, r, r.PathValue("recordId")) }

func unmanagedRecord(w http.ResponseWriter, r *http.Request, record *CloudflareRecord) bool {
	var count int64
	query := DB.WithContext(r.Context()).Model(&Monitor{}).Where("cf_zone_id = ?", r.PathValue("zoneId")).
		Where("(cf_record_id = ? AND cf_record_id <> '') OR (cf_domain = ? AND dns_type = ?)", record.ID,
			strings.ToLower(strings.TrimSuffix(record.Name, ".")), strings.ToUpper(record.Type))
	if err := query.Count(&count).Error; err != nil {
		apiError(w, 500, err)
		return false
	}
	if count > 0 {
		apiError(w, http.StatusConflict, errors.New("this DNS record is managed by a monitor; edit or delete its monitor first"))
		return false
	}
	return true
}

func saveRecord(w http.ResponseWriter, r *http.Request, id string) {
	var payload map[string]any
	if err := decodeJSON(r, &payload); err != nil {
		apiError(w, 400, err)
		return
	}
	managementMutex.Lock()
	defer managementMutex.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	auth := credentials(GetActiveAccountConfig())
	candidate := &CloudflareRecord{}
	if id != "" {
		record, err := cfClient.Record(ctx, auth, r.PathValue("zoneId"), id)
		if err != nil {
			apiError(w, http.StatusBadGateway, err)
			return
		}
		if !unmanagedRecord(w, r, record) {
			return
		}
		*candidate = *record
	}
	if name, ok := payload["name"].(string); ok {
		candidate.Name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
		// Require the same fully-qualified identity used by monitor ownership
		// checks; provider expansion of "@" or short names must not bypass them.
		if !strings.Contains(candidate.Name, ".") || !validHostname(strings.TrimPrefix(candidate.Name, "*.")) {
			apiError(w, http.StatusBadRequest, errors.New("record name must be a full DNS name, such as api.example.com"))
			return
		}
		payload["name"] = candidate.Name
	}
	if kind, ok := payload["type"].(string); ok {
		candidate.Type = strings.ToUpper(strings.TrimSpace(kind))
		payload["type"] = candidate.Type
	}
	if !unmanagedRecord(w, r, candidate) {
		return
	}
	record, err := cfClient.SaveRecord(ctx, auth, r.PathValue("zoneId"), id, payload)
	if err != nil {
		apiError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func DeleteRecord(w http.ResponseWriter, r *http.Request) {
	managementMutex.Lock()
	defer managementMutex.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	auth := credentials(GetActiveAccountConfig())
	record, err := cfClient.Record(ctx, auth, r.PathValue("zoneId"), r.PathValue("recordId"))
	if err != nil {
		apiError(w, http.StatusBadGateway, err)
		return
	}
	if !unmanagedRecord(w, r, record) {
		return
	}
	if err := cfClient.DeleteRecord(ctx, auth, r.PathValue("zoneId"), r.PathValue("recordId")); err != nil {
		apiError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "deleted"})
}
