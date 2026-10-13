package main

import (
	"context"
	"errors"
	"net/http"
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

func unmanagedRecord(w http.ResponseWriter, r *http.Request) bool {
	var count int64
	if err := DB.Model(&Monitor{}).Where("cf_zone_id = ? AND cf_record_id = ?", r.PathValue("zoneId"), r.PathValue("recordId")).Count(&count).Error; err != nil {
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
	if id != "" && !unmanagedRecord(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	record, err := cfClient.SaveRecord(ctx, credentials(GetActiveAccountConfig()), r.PathValue("zoneId"), id, payload)
	if err != nil {
		apiError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func DeleteRecord(w http.ResponseWriter, r *http.Request) {
	managementMutex.Lock()
	defer managementMutex.Unlock()
	if !unmanagedRecord(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := cfClient.DeleteRecord(ctx, credentials(GetActiveAccountConfig()), r.PathValue("zoneId"), r.PathValue("recordId")); err != nil {
		apiError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "deleted"})
}
