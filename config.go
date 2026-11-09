package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

//go:embed config.example.yaml
var exampleConfig embed.FS

// --- Configuration ---

type AccountConfig struct {
	Name     string `yaml:"name"`
	ApiToken string `yaml:"api_token"`
	Email    string `yaml:"email"`
	ApiKey   string `yaml:"api_key"`
}

type ServerConfig struct {
	Port                int    `yaml:"port"`
	Debug               bool   `yaml:"debug"`
	AuthEnabled         bool   `yaml:"auth_enabled"`
	JwtSecret           string `yaml:"jwt_secret"`
	MaxConcurrentChecks int    `yaml:"max_concurrent_checks"`
}

type Config struct {
	Server   ServerConfig `yaml:"server"`
	Database struct {
		Path                 string `yaml:"path"`
		HistoryRetentionDays int    `yaml:"history_retention_days"`
	} `yaml:"database"`
	ActiveAccount string             `yaml:"active_account"`
	Accounts      []AccountConfig    `yaml:"accounts"`
	Notification  NotificationConfig `yaml:"notification"`

	// Initial Monitors for seeding
	Monitors []MonitorConfig `yaml:"monitors"`
}

type NotificationConfig struct {
	DingTalk struct {
		Enabled     bool   `yaml:"enabled" json:"enabled"`
		AccessToken string `yaml:"access_token" json:"access_token"`
		Secret      string `yaml:"secret" json:"secret"`
	} `yaml:"dingtalk" json:"dingtalk"`
	Telegram struct {
		Enabled  bool   `yaml:"enabled" json:"enabled"`
		BotToken string `yaml:"bot_token" json:"bot_token"`
		ChatID   string `yaml:"chat_id" json:"chat_id"`
	} `yaml:"telegram" json:"telegram"`
	Email struct {
		Enabled  bool   `yaml:"enabled" json:"enabled"`
		Host     string `yaml:"host" json:"host"`
		Port     int    `yaml:"port" json:"port"`
		Username string `yaml:"username" json:"username"`
		Password string `yaml:"password" json:"password"`
		To       string `yaml:"to" json:"to"`
	} `yaml:"email" json:"email"`
}

var AppConfig Config
var LoadedConfigPath string
var configMutex sync.RWMutex
var configWriteMutex sync.Mutex
var ErrConfigCreated = errors.New("configuration created at config/config.yaml; set jwt_secret and Cloudflare credentials before starting")

func ConfigSnapshot() Config {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return cloneConfig(AppConfig)
}

// Request authentication and DNS probes need small value copies, not a deep
// clone of every bootstrap monitor and schedule on each request.
func serverConfig() ServerConfig {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return AppConfig.Server
}

func notificationConfig() NotificationConfig {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return AppConfig.Notification
}

func cloneConfig(config Config) Config {
	config.Accounts = slices.Clone(config.Accounts)
	config.Monitors = slices.Clone(config.Monitors)
	for i := range config.Monitors {
		config.Monitors[i].Schedules = slices.Clone(config.Monitors[i].Schedules)
	}
	return config
}

// UpdateConfig publishes a new snapshot only after the atomic disk write succeeds.
func UpdateConfig(change func(*Config) error) error {
	configWriteMutex.Lock()
	defer configWriteMutex.Unlock()
	next := ConfigSnapshot()
	if err := change(&next); err != nil {
		return err
	}
	if err := validateConfig(&next); err != nil {
		return err
	}
	if err := saveConfig(LoadedConfigPath, next); err != nil {
		return err
	}
	configMutex.Lock()
	AppConfig = next
	configMutex.Unlock()
	return nil
}

func saveConfig(path string, config Config) error {
	if path == "" {
		path = "config/config.yaml"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("save config atomically (mount the config directory, not a single file): %w", err)
	}
	return nil
}

func LoadConfig() error {
	configWriteMutex.Lock()
	defer configWriteMutex.Unlock()
	const path = "config/config.yaml"
	data, err := os.ReadFile(path)
	if err == nil {
		config, err := parseConfig(data)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if err := validateConfig(&config); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		configMutex.Lock()
		AppConfig, LoadedConfigPath = config, path
		configMutex.Unlock()
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	data, err = exampleConfig.ReadFile("config.example.yaml")
	if err != nil {
		return err
	}
	if err := os.MkdirAll("config", 0700); err != nil {
		return err
	}
	if err := os.WriteFile("config/config.yaml", data, 0600); err != nil {
		return err
	}
	return ErrConfigCreated
}

func parseConfig(data []byte) (Config, error) {
	var config Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return config, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return config, errors.New("configuration must contain exactly one YAML document")
	}
	return config, nil
}

func validateConfig(config *Config) error {
	if config.Server.Port == 0 {
		config.Server.Port = 8099
	}
	if config.Server.Port < 1 || config.Server.Port > 65535 {
		return errors.New("server.port must be between 1 and 65535")
	}
	if config.Server.MaxConcurrentChecks == 0 {
		config.Server.MaxConcurrentChecks = 8
	}
	if config.Server.MaxConcurrentChecks < 1 || config.Server.MaxConcurrentChecks > 256 {
		return errors.New("max_concurrent_checks must be between 1 and 256")
	}
	if config.Database.HistoryRetentionDays == 0 {
		config.Database.HistoryRetentionDays = 30
	}
	if config.Database.HistoryRetentionDays < 1 || config.Database.HistoryRetentionDays > 3650 {
		return errors.New("history_retention_days must be between 1 and 3650")
	}
	if config.Server.AuthEnabled && (len(config.Server.JwtSecret) < 32 || defaultSecret(config.Server.JwtSecret)) {
		return errors.New("set server.jwt_secret to a unique password of at least 32 characters")
	}
	names := make(map[string]bool)
	for _, account := range config.Accounts {
		if account.Name == "" || strings.TrimSpace(account.Name) != account.Name || names[account.Name] {
			return errors.New("account names must be nonempty and unique")
		}
		names[account.Name] = true
	}
	if config.ActiveAccount != "" && !names[config.ActiveAccount] {
		return errors.New("active_account does not exist")
	}
	return nil
}

func defaultSecret(secret string) bool {
	return secret == "" || secret == "change-this-secret-key-in-production" || secret == "please-change-this-secret-key-in-production"
}
