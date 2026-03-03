package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
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

type Config struct {
	Server struct {
		Port        int    `yaml:"port"`
		Debug       bool   `yaml:"debug"`
		AuthEnabled bool   `yaml:"auth_enabled"`
		JwtSecret   string `yaml:"jwt_secret"`
	} `yaml:"server"`
	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
	ActiveAccount string          `yaml:"active_account"`
	Accounts      []AccountConfig `yaml:"accounts"`
	Notification  struct {
		DingTalk struct {
			Enabled     bool   `yaml:"enabled"`
			AccessToken string `yaml:"access_token"`
			Secret      string `yaml:"secret"`
		} `yaml:"dingtalk"`
		Telegram struct {
			Enabled  bool   `yaml:"enabled"`
			BotToken string `yaml:"bot_token"`
			ChatID   string `yaml:"chat_id"`
		} `yaml:"telegram"`
		Email struct {
			Enabled  bool   `yaml:"enabled"`
			Host     string `yaml:"host"`
			Port     int    `yaml:"port"`
			Username string `yaml:"username"`
			Password string `yaml:"password"`
			To       string `yaml:"to"`
		} `yaml:"email"`
	} `yaml:"notification"`

	// Initial Monitors for seeding
	Monitors []MonitorConfig `yaml:"monitors"`
}

var AppConfig Config
var LoadedConfigPath string

func defaultConfig() Config {
	var c Config
	c.Server.Port = 8099
	c.Server.Debug = false
	c.Server.AuthEnabled = true
	c.Server.JwtSecret = "please-change-this-secret-key-in-production"
	c.Database.Path = "instance/cfguard.db"
	c.Accounts = []AccountConfig{
		{
			Name:     "default",
			ApiToken: "",
			Email:    "",
			ApiKey:   "",
		},
	}
	return c
}

func SaveConfig(path string) error {
	if path == "" {
		path = LoadedConfigPath
	}
	if path == "" {
		path = "config/config.yaml"
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&AppConfig)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadConfig() {
	configPaths := []string{"config/config.yaml", "config.yaml"}
	var lastErr error
	var foundConfig bool

	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			f, err := os.Open(path)
			if err != nil {
				log.Fatalf("Failed to open config file %s: %v", path, err)
			}
			defer f.Close()

			// First, unmarshal into a temporary map to preserve comments
			var tempMap map[string]interface{}
			decoder := yaml.NewDecoder(f)
			if err := decoder.Decode(&tempMap); err != nil {
				log.Fatalf("Failed to parse YAML from %s: %v", path, err)
			}

			// Reset file pointer and decode into the struct
			f.Seek(0, 0)
			decoder = yaml.NewDecoder(f)
			if err := decoder.Decode(&AppConfig); err != nil {
				log.Fatalf("Failed to decode config from %s into struct: %v", path, err)
			}

			LoadedConfigPath = path
			log.Println("✅ Loaded config from", path)
			foundConfig = true

			// Check for default JWT secret
			if AppConfig.Server.JwtSecret == "please-change-this-secret-key-in-production" || AppConfig.Server.JwtSecret == "change-this-secret-key-in-production" {
				log.Println("⚠️ WARNING: Your JWT secret is set to the default value.")
				log.Println("   Please change 'jwt_secret' in your config file for security.")
			}
			return
		} else {
			lastErr = err
		}
	}

	// If no config file was found, create a default one
	if !foundConfig {
		log.Println("🤔 No config file found, creating a default one...")
		LoadedConfigPath = "config/config.yaml"

		// Create the directory if it doesn't exist
		dir := filepath.Dir(LoadedConfigPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("❌ Failed to create config directory: %v", err)
		}

		// Read the embedded example config
		exampleBytes, err := exampleConfig.ReadFile("config.example.yaml")
		if err != nil {
			log.Fatalf("❌ Failed to read embedded example config: %v", err)
		}

		// Write the example config to the new file
		err = os.WriteFile(LoadedConfigPath, exampleBytes, 0644)
		if err != nil {
			log.Fatalf("❌ Failed to write default config file: %v", err)
		}

		log.Println("✅ Default config file created at:", LoadedConfigPath)
		log.Println("👉 Please edit this file with your Cloudflare details and restart the application.")
		os.Exit(0) // Exit after creating the config to force user to edit it
	}

	if lastErr != nil && !os.IsNotExist(lastErr) {
		log.Printf("⚠️ An error occurred while searching for config files: %v", lastErr)
	}
}
