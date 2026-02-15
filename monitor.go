package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// --- Engine ---

var (
	Scheduler      *cron.Cron
	schedulerMutex sync.Mutex

	// HTTP Client Cache to reuse connections (Keep-Alive)
	httpClientMutex sync.Mutex
	httpClients     = make(map[string]*http.Client)
)

func getHTTPClient(forceIP string, timeout int) *http.Client {
	httpClientMutex.Lock()
	defer httpClientMutex.Unlock()

	// Key based on configuration.
	// Note: If monitors have same forceIP but different timeouts, they need different clients
	// because http.Client.Timeout is struct field.
	key := fmt.Sprintf("%s-%d", forceIP, timeout)

	if client, ok := httpClients[key]; ok {
		return client
	}

	tr := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, // Monitor might check self-signed
		DisableKeepAlives:   false,                                 // Enable Keep-Alive
		MaxIdleConnsPerHost: 10,                                    // Allow concurrent checks to same host
		IdleConnTimeout:     90 * time.Second,
	}

	// If forceIP is provided, override DNS resolution
	if forceIP != "" {
		dialer := &net.Dialer{
			Timeout:   5 * time.Second, // TCP Connect timeout
			KeepAlive: 30 * time.Second,
		}
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			// addr is "hostname:port".
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				// Fallback if parsing fails
				return dialer.DialContext(ctx, network, addr)
			}
			// Use forceIP but keep the port
			return dialer.DialContext(ctx, network, net.JoinHostPort(forceIP, port))
		}
	}

	client := &http.Client{
		Timeout:   time.Duration(timeout) * time.Second,
		Transport: tr,
	}
	httpClients[key] = client
	return client
}

func StartScheduler() {
	ReloadSchedules()
}

func StopScheduler() {
	schedulerMutex.Lock()
	defer schedulerMutex.Unlock()

	if Scheduler != nil {
		ctx := Scheduler.Stop()
		<-ctx.Done() // Wait for running jobs to complete
		log.Println("Scheduler stopped and all jobs completed.")
	}
}

func ReloadSchedules() {
	schedulerMutex.Lock()
	defer schedulerMutex.Unlock()

	if Scheduler != nil {
		Scheduler.Stop()
	}
	Scheduler = cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cron.DefaultLogger),
	))
	Scheduler.Start()

	var monitors []Monitor
	DB.Preload("Schedules").Find(&monitors)

	// 1. Monitoring Jobs
	for _, m := range monitors {
		mCopy := m
		mCopy.ApplyDefaults()
		interval := mCopy.Interval

		if _, err := Scheduler.AddFunc(fmt.Sprintf("@every %ds", interval), func() {
			CheckMonitor(&mCopy)
		}); err != nil {
			log.Printf("Failed to schedule monitor %d: %v", mCopy.ID, err)
		}

		// 2. Schedule Jobs
		for _, s := range mCopy.Schedules {
			monitorID := mCopy.ID
			targetIP := s.TargetIP
			cronExpr := s.Cron
			if _, err := Scheduler.AddFunc(cronExpr, func() {
				ScheduledSwitch(monitorID, targetIP)
			}); err != nil {
				log.Printf("Failed to schedule switch for monitor %d: %v", monitorID, err)
			}
		}
	}

	log.Printf("Scheduler reloaded. Monitoring %d targets.", len(monitors))
}

func ScheduledSwitch(monitorID uint, targetIP string) {
	var m Monitor
	if err := DB.First(&m, monitorID).Error; err != nil {
		log.Println("ScheduledSwitch: Monitor not found", monitorID)
		return
	}

	// Avoid switching if failover is active (Status == Down)
	if m.Status == "Down" {
		log.Printf("Skipping scheduled switch for %s because it is Down", m.Name)
		return
	}

	log.Printf("Executing scheduled switch for %s to %s", m.Name, targetIP)

	// Update DNS
	if UpdateCloudflareDNS(&m, targetIP) {
		m.CurrentIP = targetIP
		m.FailCount = 0
		m.SuccCount = 0
		DB.Model(&m).Select("CurrentIP", "FailCount", "SuccCount").Updates(&m)
		SendNotification(fmt.Sprintf("🕒 计划任务: %s 已切换至 IP %s", m.Name, targetIP))
	}
}

func CheckMonitor(m *Monitor) {
	// Re-fetch monitor from DB to get latest state (avoid stale state in closure)
	var currentMonitor Monitor
	if err := DB.First(&currentMonitor, m.ID).Error; err != nil {
		return // Monitor might be deleted
	}
	*m = currentMonitor
	m.ApplyDefaults() // Ensure defaults are applied even if DB has zero values

	// We ALWAYS want to check the OriginalIP (Primary Service) availability
	// This prevents DNS caching issues and ensures we are monitoring the actual backend.
	// Even if we are currently "Down" (using Backup), we check Primary to see if it recovered.
	checkTarget := m.OriginalIP
	if checkTarget == "" {
		checkTarget = m.Target // Fallback if no specific IP configured
	}

	isUp := false
	switch m.Type {
	case "ping":
		isUp = CheckPing(checkTarget, m.Timeout)
	case "http", "https":
		// Pass OriginalIP to force connection to Primary
		isUp = CheckHTTP(m.Target, m.Timeout, m.OriginalIP)
	default:
		isUp = CheckPing(checkTarget, m.Timeout) // Default
	}

	// Logic for Failover
	if isUp {
		HandleSuccess(m)
	} else {
		HandleFailure(m)
	}

	// Update DB - Only update dynamic state fields to avoid overwriting configuration changes
	m.LastCheck = time.Now()
	// Using Select ensures we only update the fields we care about, protecting Config fields.
	// Note: We need to use Updates with a struct or map. Since m is a struct and we set fields on it,
	// Updates(m) works but we must combine it with Select to restrict columns.
	DB.Model(m).Select("Status", "LastCheck", "FailCount", "SuccCount", "CurrentIP").Updates(m)
}

func CheckHTTP(target string, timeout int, forceIP string) bool {
	if !strings.HasPrefix(target, "http") {
		target = "http://" + target
	}

	client := getHTTPClient(forceIP, timeout)

	// Use a context for safety, though client.Timeout handles it too.
	// client.Timeout is "hard" timeout.
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		log.Printf("Failed to create HTTP request for %s: %v", target, err)
		return false
	}
	// Add a user agent
	req.Header.Set("User-Agent", "CFGuard-Monitor/1.0")

	resp, err := client.Do(req)
	if err != nil {
		if AppConfig.Server.Debug {
			log.Printf("HTTP Check failed for %s: %v", target, err)
		}
		return false
	}
	defer resp.Body.Close()
	// Read a bit of body to ensure connection can be reused (drain body)
	io.Copy(io.Discard, resp.Body)

	success := resp.StatusCode >= 200 && resp.StatusCode < 400
	if !success && AppConfig.Server.Debug {
		log.Printf("HTTP Check status code error for %s: %d", target, resp.StatusCode)
	}
	return success
}

func CheckPing(host string, timeout int) bool {
	// Simple Ping implementation using OS command
	// In production, might want to use a library or raw socket, but permissions can be tricky in docker.
	// OS command is safer for unprivileged containers if ping is installed.

	// Use context with timeout slightly larger than ping timeout to kill hung processes
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+2)*time.Second)
	defer cancel()

	// Try 3 times, if 1 success then OK. This avoids flakiness.
	success := false
	for i := 0; i < 3; i++ {
		var cmd *exec.Cmd
		timeoutStr := strconv.Itoa(timeout)

		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-w", strconv.Itoa(timeout*1000), host)
		} else {
			// Check if IPv6
			cmdName := "ping"
			// Simple heuristic: if it contains a colon, treat as IPv6.
			// Note: If host is a domain, this won't trigger, which is fine as 'ping' usually handles domains.
			// But for explicit IPv6 literals, we might need ping6 on some older systems.
			// On Alpine with iputils, ping handles both.
			if strings.Contains(host, ":") {
				// Try ping6 if available, or rely on ping auto-detect
				// For compatibility, let's stick to 'ping' as iputils usually handles it.
				// However, explicitly using -6 might be safer if we want to force it?
				// Let's just use "ping" as it's standard now.
			}
			cmd = exec.CommandContext(ctx, cmdName, "-c", "1", "-W", timeoutStr, host)
		}

		// Hide output to keep logs clean
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard

		err := cmd.Run()
		if err == nil {
			success = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	return success
}

func HandleSuccess(m *Monitor) {
	if m.Status == "Down" {
		m.SuccCount++

		threshold := m.RecoveryRetries
		if threshold == 0 {
			threshold = m.Retries // Fallback to failure threshold
			if threshold == 0 {
				threshold = 3 // Default
			}
		}

		if m.SuccCount >= threshold {
			// Restore
			log.Printf("Monitor %s restored!", m.Name)

			// Try to switch DNS first
			if UpdateCloudflareDNS(m, m.OriginalIP) {
				m.Status = "Normal"
				m.SuccCount = 0
				m.CurrentIP = m.OriginalIP

				// Send Notification
				SendNotification(fmt.Sprintf("✅ 服务恢复: %s 已切回主 IP %s", m.Name, m.OriginalIP))
			} else {
				log.Printf("Monitor %s restored but failed to switch DNS to %s", m.Name, m.OriginalIP)
				// Reset SuccCount so we don't loop tightly, but keep Status=Down
				// Or maybe keep SuccCount high to retry immediately?
				// Let's keep it high.
			}
		}
	} else {
		m.FailCount = 0
	}
}

func HandleFailure(m *Monitor) {
	if m.Status == "Normal" {
		m.FailCount++
		if m.FailCount >= m.Retries {
			// Failover
			log.Printf("Monitor %s failed!", m.Name)

			// Try to switch DNS first
			if UpdateCloudflareDNS(m, m.BackupIP) {
				m.Status = "Down"
				m.FailCount = 0
				m.CurrentIP = m.BackupIP

				// Send Notification
				SendNotification(fmt.Sprintf("🚨 服务报警: %s 故障，已切换至备用 IP %s", m.Name, m.BackupIP))
			} else {
				log.Printf("Monitor %s failed but failed to switch DNS to %s", m.Name, m.BackupIP)
				// Keep status as Normal so we retry next time
			}
		}
	} else {
		m.SuccCount = 0
	}
}
