package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsInvalidConfigurationBodies(t *testing.T) {
	for _, body := range []string{"null", "[]", `{"typo":1}`, `{} {}`, `{"enabled":true} trailing`} {
		t.Run(body, func(t *testing.T) {
			var value struct {
				Enabled bool `json:"enabled"`
			}
			if err := decodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(body)), &value); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	var value struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(httptest.NewRequest("POST", "/", strings.NewReader(`{"enabled":true}`)), &value); err != nil || !value.Enabled {
		t.Fatalf("valid configuration rejected: %+v %v", value, err)
	}
}
