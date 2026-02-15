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

	r.StaticFS("/static", http.FS(staticFiles))

	r.GET("/", func(c *gin.Context) {
		c.FileFromFS("index.html", http.FS(staticFiles))
	})
	r.GET("/login", func(c *gin.Context) {
		c.FileFromFS("login.html", http.FS(staticFiles))
	})
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.FileFromFS("favicon.ico", http.FS(staticFiles))
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
			authorized.GET("/monitors", GetMonitors)
			authorized.POST("/monitors", CreateMonitor)
			authorized.PUT("/monitors/:id", UpdateMonitor)
			authorized.DELETE("/monitors/:id", DeleteMonitor)
			authorized.POST("/monitors/:id/restore", RestoreMonitor)
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
