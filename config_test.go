package main

import (
	"os"
	"testing"
	"time"
)

func TestConfigReadersStayAvailableAndWritersDoNotLoseUpdates(t *testing.T) {
	testDatabase(t)
	initial := serverConfig().Port
	entered, release := make(chan struct{}), make(chan struct{})
	first, second := make(chan error, 1), make(chan error, 1)
	go func() {
		first <- UpdateConfig(func(config *Config) error {
			close(entered)
			<-release
			config.Server.Port++
			return nil
		})
	}()
	<-entered
	go func() {
		second <- UpdateConfig(func(config *Config) error { config.Server.Port++; return nil })
	}()
	// Always release writers before testDatabase restores global configuration.
	defer func() {
		close(release)
		if err := <-first; err != nil {
			t.Error(err)
		}
		if err := <-second; err != nil {
			t.Error(err)
		}
		if got := serverConfig().Port; got != initial+2 {
			t.Errorf("concurrent update lost: port=%d", got)
		}
		data, err := os.ReadFile(LoadedConfigPath)
		if err != nil {
			t.Error(err)
			return
		}
		disk, err := parseConfig(data)
		if err != nil || disk.Server.Port != initial+2 {
			t.Errorf("disk/live configuration mismatch: port=%d, err=%v", disk.Server.Port, err)
		}
	}()
	read := make(chan int, 1)
	go func() { read <- serverConfig().Port }()
	select {
	case port := <-read:
		if port != initial {
			t.Fatal("uncommitted configuration was visible")
		}
	case <-time.After(time.Second):
		t.Error("configuration writer blocked authentication/probe readers")
	}
}

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
