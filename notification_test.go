package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNotificationRequiresCompleteExplicitSuccess(t *testing.T) {
	previous := notifyClient
	t.Cleanup(func() { notifyClient = previous })
	for _, tc := range []struct {
		name, body        string
		telegram, success bool
	}{
		{"dingtalk success", `{"errcode":0,"errmsg":"ok"}`, false, true},
		{"telegram success", `{"ok":true}`, true, true},
		{"missing code", `{}`, false, false},
		{"null response", `null`, false, false},
		{"null code", `{"errcode":null}`, false, false},
		{"provider rejection", `{"errcode":310000}`, false, false},
		{"telegram rejection", `{"ok":false}`, true, false},
		{"trailing data", `{"errcode":0} invalid`, false, false},
		{"multiple responses", `{"ok":true} {"ok":false}`, true, false},
		{"oversized response", `{"errcode":0,"padding":"` + strings.Repeat("x", 16<<10) + `"}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notifyClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			err := postNotification(context.Background(), "https://notify.invalid/", map[string]string{"text": "test"}, tc.telegram)
			if (err == nil) != tc.success {
				t.Fatalf("success=%v, error=%v", tc.success, err)
			}
		})
	}
}
