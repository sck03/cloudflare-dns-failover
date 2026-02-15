package main

import (
	"fmt"
	"log"
	"net/http"
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
	monitor.BackupIP = input.BackupIP

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
