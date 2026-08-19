package main

import (
	"bufio"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func loadEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || os.Getenv(strings.TrimSpace(key)) != "" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		os.Setenv(strings.TrimSpace(key), value)
	}
}

func envBool(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes"
}

func main() {
	loadEnv(".env")
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	port := os.Getenv("PORT")
	if port == "" {
		port = "5510"
	}
	pythonURL := os.Getenv("PYTHON_SERVICE_URL")
	if pythonURL == "" {
		pythonURL = "http://127.0.0.1:8091"
	}
	root, err := filepath.Abs("public")
	if err != nil {
		logger.Error("public path error", "error", err)
		os.Exit(1)
	}
	origins := strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",")
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	publicBaseURL := os.Getenv("PUBLIC_BASE_URL")
	auth := NewAuthStore(clientID, envBool("COOKIE_SECURE", false), os.Getenv("SESSION_STORE_PATH"))
	rooms := NewRoomManager(NewHotelService(strings.TrimRight(pythonURL, "/")))
	server := NewServer(root, clientID, clientSecret, publicBaseURL, auth, rooms, logger, origins)
	httpServer := &http.Server{Addr: ":" + port, Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 90 * time.Second}
	logger.Info("Tutu Monopoly started", "url", "http://localhost:"+port+"/game")
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
