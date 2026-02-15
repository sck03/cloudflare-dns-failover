package main

import (
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

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
	Accounts     []AccountConfig `yaml:"accounts"`
	Notification struct {
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

func LoadConfig() {
	// Set Defaults
	AppConfig.Server.AuthEnabled = true

	f, err := os.Open("config.yaml")
	if err != nil {
		log.Println("config.yaml not found, using defaults")
		AppConfig.Server.Port = 8099
		AppConfig.Database.Path = "instance/cfguard.db"
		return
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&AppConfig)
	if err != nil {
		log.Fatal("Failed to parse config.yaml:", err)
	}
}
