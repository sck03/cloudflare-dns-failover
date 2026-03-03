package main

import (
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// --- Controllers ---

func GetMonitors(c *gin.Context) {
	var monitors []Monitor
	DB.Preload("Schedules").Find(&monitors)
	c.JSON(http.StatusOK, monitors)
}

func GetStatus(c *gin.Context) {
	var monitors []Monitor
	DB.Find(&monitors)

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	c.JSON(http.StatusOK, gin.H{
		"system": gin.H{
			"goroutines": runtime.NumGoroutine(),
			"mem_alloc":  mem.Alloc,
		},
		"monitors": monitors,
	})
}

func GetZones(c *gin.Context) {
	acc := GetActiveAccountConfig()
	if acc == nil {
		c.JSON(http.StatusOK, []CloudflareZone{})
		return
	}
	zones, err := FetchCloudflareZones(acc)
	if err != nil {
		log.Printf("Failed to fetch zones: %v", err)
		c.JSON(http.StatusOK, []CloudflareZone{})
		return
	}
	c.JSON(http.StatusOK, zones)
}

func GetZoneRecords(c *gin.Context) {
	zoneID := c.Param("zoneId")
	acc := GetActiveAccountConfig()
	if acc == nil {
		c.JSON(http.StatusOK, []CloudflareRecord{})
		return
	}
	records, err := FetchCloudflareRecords(acc, zoneID)
	if err != nil {
		log.Printf("Failed to fetch zone records: %v", err)
		c.JSON(http.StatusOK, []CloudflareRecord{})
		return
	}
	c.JSON(http.StatusOK, records)
}

type ConfigPayload struct {
	Cloudflare struct {
		ApiToken string `json:"api_token"`
	} `json:"cloudflare"`
	DingTalk struct {
		Enabled     bool   `json:"enabled"`
		AccessToken string `json:"access_token"`
		Secret      string `json:"secret"`
	} `json:"dingtalk"`
	Telegram struct {
		Enabled  bool   `json:"enabled"`
		BotToken string `json:"bot_token"`
		ChatID   string `json:"chat_id"`
	} `json:"telegram"`
	Email struct {
		Enabled  bool   `json:"enabled"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		To       string `json:"to"`
	} `json:"email"`
}

func GetConfig(c *gin.Context) {
	acc := GetActiveAccountConfig()
	payload := ConfigPayload{}
	if acc != nil {
		payload.Cloudflare.ApiToken = acc.ApiToken
	}
	payload.DingTalk.Enabled = AppConfig.Notification.DingTalk.Enabled
	payload.DingTalk.AccessToken = AppConfig.Notification.DingTalk.AccessToken
	payload.DingTalk.Secret = AppConfig.Notification.DingTalk.Secret
	payload.Telegram.Enabled = AppConfig.Notification.Telegram.Enabled
	payload.Telegram.BotToken = AppConfig.Notification.Telegram.BotToken
	payload.Telegram.ChatID = AppConfig.Notification.Telegram.ChatID
	payload.Email.Enabled = AppConfig.Notification.Email.Enabled
	payload.Email.Host = AppConfig.Notification.Email.Host
	payload.Email.Port = AppConfig.Notification.Email.Port
	payload.Email.Username = AppConfig.Notification.Email.Username
	payload.Email.Password = AppConfig.Notification.Email.Password
	payload.Email.To = AppConfig.Notification.Email.To
	c.JSON(http.StatusOK, payload)
}

func SaveConfigHandler(c *gin.Context) {
	var input ConfigPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	acc := GetActiveAccountConfig()
	if acc == nil {
		AppConfig.Accounts = []AccountConfig{{Name: "default", ApiToken: input.Cloudflare.ApiToken}}
	} else if input.Cloudflare.ApiToken != "" {
		acc.ApiToken = input.Cloudflare.ApiToken
	}

	AppConfig.Notification.DingTalk.Enabled = input.DingTalk.Enabled
	AppConfig.Notification.DingTalk.AccessToken = input.DingTalk.AccessToken
	AppConfig.Notification.DingTalk.Secret = input.DingTalk.Secret
	AppConfig.Notification.Telegram.Enabled = input.Telegram.Enabled
	AppConfig.Notification.Telegram.BotToken = input.Telegram.BotToken
	AppConfig.Notification.Telegram.ChatID = input.Telegram.ChatID
	AppConfig.Notification.Email.Enabled = input.Email.Enabled
	AppConfig.Notification.Email.Host = input.Email.Host
	AppConfig.Notification.Email.Port = input.Email.Port
	AppConfig.Notification.Email.Username = input.Email.Username
	AppConfig.Notification.Email.Password = input.Email.Password
	AppConfig.Notification.Email.To = input.Email.To

	if err := SaveConfig(""); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "saved"})
}

type AccountPayload struct {
	Name     string `json:"name"`
	ApiToken string `json:"api_token"`
	Email    string `json:"email"`
	ApiKey   string `json:"api_key"`
}

type AccountView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ApiToken string `json:"api_token"`
	Email    string `json:"email"`
	ApiKey   string `json:"api_key"`
}

func GetCloudflareAccounts(c *gin.Context) {
	views := make([]AccountView, 0, len(AppConfig.Accounts))
	for _, acc := range AppConfig.Accounts {
		views = append(views, AccountView{
			ID:       acc.Name,
			Name:     acc.Name,
			ApiToken: acc.ApiToken,
			Email:    acc.Email,
			ApiKey:   acc.ApiKey,
		})
	}
	activeIndex := 0
	if AppConfig.ActiveAccount != "" {
		for i, acc := range AppConfig.Accounts {
			if acc.Name == AppConfig.ActiveAccount {
				activeIndex = i
				break
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"accounts": views, "active_index": activeIndex})
}

func CreateCloudflareAccount(c *gin.Context) {
	var input AccountPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	for i := range AppConfig.Accounts {
		if AppConfig.Accounts[i].Name == input.Name {
			AppConfig.Accounts[i].ApiToken = input.ApiToken
			AppConfig.Accounts[i].Email = input.Email
			AppConfig.Accounts[i].ApiKey = input.ApiKey
			if err := SaveConfig(""); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "updated"})
			return
		}
	}
	AppConfig.Accounts = append(AppConfig.Accounts, AccountConfig{
		Name:     input.Name,
		ApiToken: input.ApiToken,
		Email:    input.Email,
		ApiKey:   input.ApiKey,
	})
	if AppConfig.ActiveAccount == "" {
		AppConfig.ActiveAccount = input.Name
	}
	if err := SaveConfig(""); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "created"})
}

func UpdateCloudflareAccount(c *gin.Context) {
	id := c.Param("id")
	var input AccountPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	for i := range AppConfig.Accounts {
		if AppConfig.Accounts[i].Name == id {
			if input.Name != "" && input.Name != id {
				AppConfig.Accounts[i].Name = input.Name
			}
			if input.ApiToken != "" {
				AppConfig.Accounts[i].ApiToken = input.ApiToken
			}
			if input.Email != "" {
				AppConfig.Accounts[i].Email = input.Email
			}
			if input.ApiKey != "" {
				AppConfig.Accounts[i].ApiKey = input.ApiKey
			}
			if AppConfig.ActiveAccount == id && input.Name != "" && input.Name != id {
				AppConfig.ActiveAccount = input.Name
			}
			if err := SaveConfig(""); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "updated"})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
}

func DeleteCloudflareAccount(c *gin.Context) {
	id := c.Param("id")
	index := -1
	for i, acc := range AppConfig.Accounts {
		if acc.Name == id {
			index = i
			break
		}
	}
	if index == -1 {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	AppConfig.Accounts = append(AppConfig.Accounts[:index], AppConfig.Accounts[index+1:]...)
	if AppConfig.ActiveAccount == id {
		if len(AppConfig.Accounts) > 0 {
			AppConfig.ActiveAccount = AppConfig.Accounts[0].Name
		} else {
			AppConfig.ActiveAccount = ""
		}
	}
	if err := SaveConfig(""); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func ActivateCloudflareAccount(c *gin.Context) {
	id := c.Param("id")
	for _, acc := range AppConfig.Accounts {
		if acc.Name == id {
			AppConfig.ActiveAccount = id
			if err := SaveConfig(""); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "activated"})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
}
func CreateMonitor(c *gin.Context) {
	var input MonitorConfig
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.Name == "" || input.Target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name and Target are required"})
		return
	}

	monitor := input.ToMonitor()
	monitor.CurrentIP = monitor.OriginalIP
	monitor.Status = "Normal"
	monitor.LastCheck = time.Now()

	// Map schedules
	for _, s := range input.Schedules {
		if s.Cron == "" || s.TargetIP == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Schedule cron and target_ip are required"})
			return
		}
		monitor.Schedules = append(monitor.Schedules, Schedule{
			Cron:     s.Cron,
			TargetIP: s.TargetIP,
		})
	}

	// Fetch Record ID if missing
	if monitor.CFRecordID == "" && monitor.CFZoneID != "" && monitor.CFDomain != "" {
		foundID, err := FetchCloudflareRecordID(&monitor)
		if err == nil && foundID != "" {
			monitor.CFRecordID = foundID
		} else {
			// Warning but allow creation? Or fail?
			// Let's allow creation but log/return warning if possible.
			// Ideally we should probably fail or return a warning field.
			// For now, let's just log it. The user can check status.
			log.Printf("Warning: Failed to fetch Record ID during creation: %v\n", err)
		}
	}

	if err := DB.Create(&monitor).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create monitor"})
		return
	}

	// Reload Scheduler
	StartScheduler()

	c.JSON(http.StatusOK, monitor)
}

func UpdateMonitor(c *gin.Context) {
	id := c.Param("id")
	var input struct {
		MonitorConfig
		ScheduleEnabled  *bool  `json:"schedule_enabled"` // Use pointer to distinguish missing vs false
		ScheduleHours    int    `json:"schedule_hours"`
		ScheduleSwitchIP string `json:"schedule_switch_ip"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if input.Name == "" || input.Target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name and Target are required"})
		return
	}

	var monitor Monitor
	if err := DB.First(&monitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Monitor not found"})
		return
	}

	// Update Fields
	monitor.Name = input.Name
	monitor.AccountName = input.Account
	monitor.Target = input.Target
	monitor.Type = input.Type
	monitor.DNSType = input.DNSType
	monitor.Interval = input.Interval
	monitor.Timeout = input.Timeout
	monitor.Retries = input.Retries
	monitor.RecoveryRetries = input.RecoveryRetries
	monitor.OriginalIP = input.OriginalIP
	monitor.OriginalIPProxy = input.OriginalIPProxy
	monitor.BackupIP = input.BackupIP
	monitor.BackupIPProxy = input.BackupIPProxy

	// Handle critical field changes that require re-fetching Record ID
	shouldFetchID := false
	if input.ZoneID != "" && input.ZoneID != monitor.CFZoneID {
		monitor.CFZoneID = input.ZoneID
		shouldFetchID = true
	}
	if input.Domain != "" && input.Domain != monitor.CFDomain {
		monitor.CFDomain = input.Domain
		shouldFetchID = true
	}

	// If user explicitly provided RecordID (rarely via UI, but possible via API), use it
	if input.RecordID != "" {
		monitor.CFRecordID = input.RecordID
		shouldFetchID = false
	} else if shouldFetchID {
		// Reset ID to force re-fetch if not provided but context changed
		monitor.CFRecordID = ""
	}

	if shouldFetchID && monitor.CFRecordID == "" {
		foundID, err := FetchCloudflareRecordID(&monitor)
		if err == nil && foundID != "" {
			monitor.CFRecordID = foundID
		} else {
			log.Printf("Warning: Failed to fetch Record ID during update: %v\n", err)
		}
	}

	monitor.ApplyDefaults()

	// Transaction to ensure atomicity
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Save Monitor updates
		if err := tx.Save(&monitor).Error; err != nil {
			return err
		}

		// Handle Schedule Logic
		// Priority:
		// 1. Explicit 'schedules' array in JSON (MonitorConfig.Schedules) -> Overwrite all.
		// 2. 'schedule_enabled' is present (Simple Mode Update) -> Logic below.
		// 3. Neither -> Do nothing (preserve existing schedules).

		// Note: We can't easily detect if 'schedules' was explicitly sent as empty list vs missing with standard struct.
		// But since we are supporting the Simple Mode via side-channel fields, we can rely on ScheduleEnabled pointer.

		if len(input.MonitorConfig.Schedules) > 0 {
			// Case 1: Explicit schedules provided
			tx.Where("monitor_id = ?", monitor.ID).Delete(&Schedule{})
			for _, s := range input.MonitorConfig.Schedules {
				if err := tx.Create(&Schedule{
					MonitorID: monitor.ID,
					Cron:      s.Cron,
					TargetIP:  s.TargetIP,
				}).Error; err != nil {
					return err
				}
			}
		} else if input.ScheduleEnabled != nil {
			// Case 2: Simple Mode Update (schedule_enabled is present)
			if *input.ScheduleEnabled {
				if input.ScheduleSwitchIP == "" {
					return fmt.Errorf("schedule_switch_ip is required")
				}
				if input.ScheduleHours < 0 || input.ScheduleHours > 23 {
					return fmt.Errorf("schedule_hours must be between 0 and 23")
				}
				// Enabled: Create the single schedule
				tx.Where("monitor_id = ?", monitor.ID).Delete(&Schedule{})
				cronExpr := fmt.Sprintf("0 %d * * *", input.ScheduleHours)
				if err := tx.Create(&Schedule{
					MonitorID: monitor.ID,
					Cron:      cronExpr,
					TargetIP:  input.ScheduleSwitchIP,
				}).Error; err != nil {
					return err
				}
			} else {
				// Disabled: Clear all schedules
				tx.Where("monitor_id = ?", monitor.ID).Delete(&Schedule{})
			}
		}
		// Case 3: Neither present (e.g. General Settings update) -> Touch nothing.
		return nil
	})

	if err != nil {
		if strings.Contains(err.Error(), "required") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update monitor: " + err.Error()})
		}
		return
	}

	// Reload Scheduler
	StartScheduler()

	c.JSON(http.StatusOK, monitor)
}

func RestoreMonitor(c *gin.Context) {
	id := c.Param("id")
	var monitor Monitor
	if err := DB.First(&monitor, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Monitor not found"})
		return
	}

	// Force restore to original IP
	monitor.Status = "Normal"
	monitor.FailCount = 0
	monitor.SuccCount = 0
	monitor.CurrentIP = monitor.OriginalIP
	monitor.LastCheck = time.Now()

	if UpdateCloudflareDNS(&monitor, monitor.OriginalIP) {
		SendNotification(fmt.Sprintf("✅ 手动恢复: %s 已切回主 IP %s", monitor.Name, monitor.OriginalIP))
	}

	DB.Save(&monitor)
	c.JSON(http.StatusOK, monitor)
}

func DeleteMonitor(c *gin.Context) {
	id := c.Param("id")

	// Transaction
	err := DB.Transaction(func(tx *gorm.DB) error {
		// Delete associated schedules first
		if err := tx.Where("monitor_id = ?", id).Delete(&Schedule{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&Monitor{}, id).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete monitor"})
		return
	}

	// Reload Scheduler
	StartScheduler()

	c.JSON(http.StatusOK, gin.H{"message": "Deleted"})
}

// --- Auth ---

type LoginRequest struct {
	Token string `json:"token"`
}

func AuthStatus(c *gin.Context) {
	// Check if "jwt_secret" is still the default/placeholder
	needSetup := AppConfig.Server.JwtSecret == "change-this-secret-key-in-production" || AppConfig.Server.JwtSecret == "please-change-this-secret-key-in-production"

	authenticated := false
	tokenString, err := c.Cookie("token")
	if err == nil && tokenString != "" {
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(AppConfig.Server.JwtSecret), nil
		})
		if err == nil && token.Valid {
			authenticated = true
		}
	}

	c.JSON(200, gin.H{
		"code": 200,
		"data": gin.H{
			"need_setup":    needSetup,
			"authenticated": authenticated,
			"auth_enabled":  AppConfig.Server.AuthEnabled,
		},
	})
}

func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "Invalid request"})
		return
	}

	// Validate Token
	// The "password" is effectively the JWT Secret itself in this simplified model,
	// OR we can add a specific password field.
	// Based on the user prompt "加JWT 密钥也能设置", it seems they want to use the Secret as the key.
	// Let's assume the user enters the Secret Key defined in config.yaml as the password.

	if req.Token != AppConfig.Server.JwtSecret {
		c.JSON(401, gin.H{"code": 401, "msg": "Invalid Token"})
		return
	}

	// Generate JWT
	claims := jwt.MapClaims{
		"authorized": true,
		"exp":        time.Now().Add(time.Hour * 24).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(AppConfig.Server.JwtSecret))
	if err != nil {
		c.JSON(500, gin.H{"code": 500, "msg": "Failed to generate token"})
		return
	}

	// Set Cookie
	c.SetCookie("token", tokenString, 3600*24, "/", "", false, true)

	c.JSON(200, gin.H{
		"code":  200,
		"msg":   "Login successful",
		"token": tokenString,
	})
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !AppConfig.Server.AuthEnabled {
			c.Next()
			return
		}

		tokenString, err := c.Cookie("token")
		if err != nil {
			// Try header
			authHeader := c.GetHeader("Authorization")
			if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
				tokenString = authHeader[7:]
			}
		}

		if tokenString == "" {
			c.JSON(401, gin.H{"code": 401, "msg": "Unauthorized"})
			c.Abort()
			return
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(AppConfig.Server.JwtSecret), nil
		})

		if err != nil || !token.Valid {
			c.JSON(401, gin.H{"code": 401, "msg": "Invalid Token"})
			c.Abort()
			return
		}

		c.Next()
	}
}
