package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed static
var embedFS embed.FS

// --- Main ---

func main() {
	LoadConfig()
	InitDB()
	SeedMonitors()

	if !AppConfig.Server.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// Serve Static Files (Embedded)
	staticFiles, err := fs.Sub(embedFS, "static")
	if err != nil {
		log.Fatal("Failed to load static files:", err)
	}

	r.StaticFS("/", http.FS(staticFiles))

	r.NoRoute(func(c *gin.Context) {
		// Check if it's a frontend route, if so, serve index.html
		if !strings.HasPrefix(c.Request.URL.Path, "/api") {
			content, err := fs.ReadFile(staticFiles, "index.html")
			if err != nil {
				c.String(http.StatusInternalServerError, "Error reading index.html")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", content)
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"code": "PAGE_NOT_FOUND", "message": "Page not found"})
	})

	// API Routes
	api := r.Group("/api")
	{
		// Auth Routes
		api.GET("/auth/check", AuthStatus)
		api.POST("/auth/login", Login)

		// Protected Routes
		authorized := api.Group("/")
		authorized.Use(AuthMiddleware())
		{
			authorized.GET("/status", GetStatus)
			authorized.GET("/monitors", GetMonitors)
			authorized.POST("/monitors", CreateMonitor)
			authorized.PUT("/monitors/:id", UpdateMonitor)
			authorized.DELETE("/monitors/:id", DeleteMonitor)
			authorized.POST("/monitors/:id/restore", RestoreMonitor)
			authorized.GET("/zones", GetZones)
			authorized.GET("/zones/:zoneId/records", GetZoneRecords)
			authorized.POST("/zones/:zoneId/records", CreateRecord)
			authorized.PUT("/zones/:zoneId/records/:recordId", UpdateRecord)
			authorized.DELETE("/zones/:zoneId/records/:recordId", DeleteRecord)
			authorized.GET("/config", GetConfig)
			authorized.POST("/config", SaveConfigHandler)
			authorized.GET("/cloudflare-accounts", GetCloudflareAccounts)
			authorized.POST("/cloudflare-accounts", CreateCloudflareAccount)
			authorized.PUT("/cloudflare-accounts/:id", UpdateCloudflareAccount)
			authorized.DELETE("/cloudflare-accounts/:id", DeleteCloudflareAccount)
			authorized.POST("/cloudflare-accounts/:id/activate", ActivateCloudflareAccount)
		}
	}

	// Start Scheduler
	StartScheduler()

	addr := fmt.Sprintf(":%d", AppConfig.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// Initializing the server in a goroutine so that
	// it won't block the graceful shutdown handling below
	go func() {
		log.Printf("Starting server on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with
	// a timeout of 5 seconds.
	quit := make(chan os.Signal, 1)
	// kill (no param) default send syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall.SIGKILL but can't be caught, so don't need to add it
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Stop Scheduler first to prevent new checks
	StopScheduler()

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exiting")
}
