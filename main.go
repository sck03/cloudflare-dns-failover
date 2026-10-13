package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"cfguard/internal/health"
)

//go:embed static
var embedFS embed.FS

var version = "dev"

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Printf("CFGuard %s (%s)\n", version, runtime.Version())
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "-healthcheck" {
		if err := checkHealth(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := run(); err != nil {
		if errors.Is(err, ErrConfigCreated) {
			log.Print(err)
			return
		}
		log.Fatal(err)
	}
}

func checkHealth() error {
	if err := LoadConfig(); err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", ConfigSnapshot().Server.Port))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health check HTTP %d", response.StatusCode)
	}
	return nil
}

func run() error {
	if err := LoadConfig(); err != nil {
		return err
	}
	if err := InitDB(); err != nil {
		return err
	}
	database, err := DB.DB()
	if err != nil {
		return err
	}
	defer database.Close()
	if err := SeedMonitors(); err != nil {
		return err
	}
	pruneHistory()
	config := ConfigSnapshot()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	checker := health.NewChecker()
	defer checker.Close()
	notifier = NewNotifier()
	defer notifier.Stop()
	engine = NewEngine(DB, checker, cloudflareDNS{}, config.Server.MaxConcurrentChecks, SendNotification)
	defer engine.Stop()
	defer cfClient.HTTP.CloseIdleConnections()
	server := &http.Server{
		Addr: fmt.Sprintf(":%d", config.Server.Port), Handler: NewRouter(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 150 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 64 << 10,
		BaseContext:    func(net.Listener) context.Context { return ctx },
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := engine.Reload(); err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() {
		log.Printf("CFGuard %s listening on %s", version, server.Addr)
		result <- server.Serve(listener)
	}()
	select {
	case err := <-result:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	// Cancelling the engine aborts probes/API backoff instead of waiting out timeouts.
	engine.Stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		server.Close()
		return err
	}
	return nil
}

func NewRouter() http.Handler {
	router := http.NewServeMux()
	staticFiles, err := fs.Sub(embedFS, "static")
	if err != nil {
		panic(err)
	}
	router.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	jsFiles, err := fs.Sub(staticFiles, "js")
	if err != nil {
		panic(err)
	}
	router.Handle("GET /js/", http.StripPrefix("/js/", http.FileServer(http.FS(jsFiles))))
	for path, file := range map[string]string{"/{$}": "index.html", "/login": "login.html", "/app.js": "app.js", "/favicon.png": "favicon.png"} {
		content, err := fs.ReadFile(staticFiles, file)
		if err != nil {
			panic(err)
		}
		contentType := mime.TypeByExtension(filepath.Ext(file))
		router.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.Write(content)
		})
	}
	router.HandleFunc("GET /login.html", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
	})
	router.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		db, err := DB.DB()
		if err == nil {
			err = db.PingContext(ctx)
		}
		if err != nil {
			apiError(w, http.StatusServiceUnavailable, errors.New("database unavailable"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": version})
	})
	router.HandleFunc("GET /api/auth/check", AuthStatus)
	router.HandleFunc("POST /api/auth/login", Login)
	api := http.NewServeMux()
	api.HandleFunc("GET /api/status", GetStatus)
	api.HandleFunc("GET /api/monitors", GetMonitors)
	api.HandleFunc("POST /api/monitors", CreateMonitor)
	api.HandleFunc("PUT /api/monitors/{id}", UpdateMonitor)
	api.HandleFunc("DELETE /api/monitors/{id}", DeleteMonitor)
	api.HandleFunc("POST /api/monitors/{id}/restore", RestoreMonitor)
	api.HandleFunc("POST /api/monitors/{id}/schedule", SaveSchedule)
	api.HandleFunc("GET /api/zones", GetZones)
	api.HandleFunc("GET /api/zones/{zoneId}/records", GetZoneRecords)
	api.HandleFunc("POST /api/zones/{zoneId}/records", CreateRecord)
	api.HandleFunc("PUT /api/zones/{zoneId}/records/{recordId}", UpdateRecord)
	api.HandleFunc("DELETE /api/zones/{zoneId}/records/{recordId}", DeleteRecord)
	api.HandleFunc("GET /api/config", GetConfig)
	api.HandleFunc("POST /api/config", SaveConfigHandler)
	api.HandleFunc("GET /api/cloudflare-accounts", GetCloudflareAccounts)
	api.HandleFunc("POST /api/cloudflare-accounts", CreateCloudflareAccount)
	api.HandleFunc("PUT /api/cloudflare-accounts/{id}", UpdateCloudflareAccount)
	api.HandleFunc("DELETE /api/cloudflare-accounts/{id}", DeleteCloudflareAccount)
	api.HandleFunc("POST /api/cloudflare-accounts/{id}/activate", ActivateCloudflareAccount)
	router.Handle("/api/", AuthMiddleware(api))
	debug := ConfigSnapshot().Server.Debug
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if debug {
			log.Printf("%s %q", r.Method, r.URL.Path)
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Host != r.Host {
					apiError(w, http.StatusForbidden, errors.New("cross-origin request rejected"))
					return
				}
			}
		}
		router.ServeHTTP(w, r)
	})
}
