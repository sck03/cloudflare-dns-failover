package main

import "testing"

func TestConfigRejectsTyposAndMultipleDocuments(t *testing.T) {
	for _, input := range []string{
		"server:\n  max_concurent_checks: 8\n",
		"server:\n  port: 8099\n---\nserver:\n  port: 80\n",
		"",
	} {
		if _, err := parseConfig([]byte(input)); err == nil {
			t.Errorf("accepted invalid config: %q", input)
		}
	}
	data, err := exampleConfig.ReadFile("config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseConfig(data); err != nil {
		t.Fatalf("example configuration: %v", err)
	}
}
