package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type Server struct {
	publicRoot         string
	googleClientID     string
	googleClientSecret string
	publicBaseURL      string
	auth               *AuthStore
	rooms              *RoomManager
	logger             *slog.Logger
	allowedOrigins     map[string]struct{}
	upgrader           websocket.Upgrader
}

func NewServer(publicRoot string, clientID string, clientSecret string, publicBaseURL string, auth *AuthStore, rooms *RoomManager, logger *slog.Logger, allowedOrigins []string) *Server {
	origins := map[string]struct{}{}
	for _, origin := range allowedOrigins {
		if value := strings.TrimSpace(origin); value != "" {
			origins[strings.TrimRight(value, "/")] = struct{}{}
		}
	}
	server := &Server{publicRoot: publicRoot, googleClientID: clientID, googleClientSecret: clientSecret, publicBaseURL: strings.TrimRight(publicBaseURL, "/"), auth: auth, rooms: rooms, logger: logger, allowedOrigins: origins}
	server.upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: server.checkOrigin}
	return server
}

func (server *Server) checkOrigin(request *http.Request) bool {
	origin := strings.TrimRight(request.Header.Get("Origin"), "/")
	if origin == "" {
		return true
	}
	if _, ok := server.allowedOrigins[origin]; ok {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, request.Host)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(response, request)
	})
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", server.health)
	mux.HandleFunc("GET /scripts/app-config.js", server.appConfig)
	mux.HandleFunc("GET /api/countries", server.listCountries)
	mux.HandleFunc("POST /api/auth/google", server.googleLogin)
	mux.HandleFunc("GET /api/auth/google/start", server.googleStart)
	mux.HandleFunc("GET /api/auth/google/callback", server.googleCallback)
	mux.HandleFunc("GET /api/auth/me", server.me)
	mux.HandleFunc("POST /api/auth/logout", server.logout)
	mux.HandleFunc("GET /api/rooms", server.listRooms)
	mux.HandleFunc("POST /api/rooms", server.createRoom)
	mux.HandleFunc("POST /api/rooms/{id}/join", server.joinRoom)
	mux.HandleFunc("POST /api/rooms/{id}/leave", server.leaveRoom)
	mux.HandleFunc("GET /api/rooms/{id}", server.getRoom)
	mux.HandleFunc("POST /api/rooms/{id}/actions/{action}", server.roomAction)
	mux.HandleFunc("GET /api/rooms/{id}/ws", server.roomSocket)
	mux.HandleFunc("GET /", server.static)
	return securityHeaders(mux)
}

func sendJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	json.NewEncoder(response).Encode(value)
}

func sendError(response http.ResponseWriter, status int, err error) {
	sendJSON(response, status, map[string]string{"error": err.Error()})
}

func (server *Server) requireUser(response http.ResponseWriter, request *http.Request) (User, bool) {
	user, ok := server.auth.User(request)
	if !ok {
		sendError(response, http.StatusUnauthorized, errors.New("нужно войти через Google"))
		return User{}, false
	}
	return user, true
}

func (server *Server) health(response http.ResponseWriter, _ *http.Request) {
	sendJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) listCountries(response http.ResponseWriter, request *http.Request) {
	values, err := server.rooms.hotels.Countries(request.Context())
	if err != nil {
		server.logger.Warn("country catalog unavailable", "error", err)
		values = countries()
	}
	sendJSON(response, http.StatusOK, map[string]any{"countries": values})
}

func (server *Server) appConfig(response http.ResponseWriter, _ *http.Request) {
	value, _ := json.Marshal(server.googleClientID)
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(response, "window.GOOGLE_CLIENT_ID = %s;", value)
}

func (server *Server) googleLogin(response http.ResponseWriter, request *http.Request) {
	if server.googleClientID == "" {
		sendError(response, http.StatusServiceUnavailable, errors.New("GOOGLE_CLIENT_ID не настроен"))
		return
	}
	if !server.checkOrigin(request) {
		sendError(response, http.StatusForbidden, errors.New("недопустимый origin"))
		return
	}
	var input struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(io.LimitReader(request.Body, 16<<10)).Decode(&input); err != nil || input.Credential == "" {
		sendError(response, http.StatusBadRequest, errors.New("Google credential обязателен"))
		return
	}
	claims, err := server.auth.verifier.Verify(request.Context(), input.Credential)
	if err != nil {
		server.logger.Warn("google login rejected", "error", err)
		sendError(response, http.StatusUnauthorized, errors.New("Google не подтвердил вход"))
		return
	}
	name := claims.Name
	if name == "" {
		name = claims.GivenName
	}
	if name == "" {
		name = claims.Email
	}
	user := User{ID: claims.Subject, Email: claims.Email, Name: name, Picture: claims.Picture}
	token, err := server.auth.Create(user)
	if err != nil {
		sendError(response, http.StatusInternalServerError, errors.New("не удалось создать сессию"))
		return
	}
	server.auth.SetCookie(response, token)
	sendJSON(response, http.StatusOK, map[string]any{"user": user})
}

func (server *Server) me(response http.ResponseWriter, request *http.Request) {
	user, ok := server.auth.User(request)
	if !ok {
		sendJSON(response, http.StatusUnauthorized, map[string]any{"user": nil})
		return
	}
	sendJSON(response, http.StatusOK, map[string]any{"user": user})
}

func (server *Server) logout(response http.ResponseWriter, request *http.Request) {
	server.auth.Logout(response, request)
	sendJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (server *Server) listRooms(response http.ResponseWriter, request *http.Request) {
	sendJSON(response, http.StatusOK, map[string]any{"rooms": server.rooms.List()})
}

func (server *Server) createRoom(response http.ResponseWriter, request *http.Request) {
	user, ok := server.requireUser(response, request)
	if !ok {
		return
	}
	var input CreateRoomInput
	decoder := json.NewDecoder(io.LimitReader(request.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		sendError(response, http.StatusBadRequest, errors.New("некорректные настройки лобби"))
		return
	}
	room, err := server.rooms.Create(request.Context(), user, input)
	if err != nil {
		server.logger.Error("room creation failed", "error", err)
		sendError(response, http.StatusBadGateway, err)
		return
	}
	sendJSON(response, http.StatusCreated, map[string]any{"room": room.Snapshot(user.ID)})
}

func (server *Server) joinRoom(response http.ResponseWriter, request *http.Request) {
	user, ok := server.requireUser(response, request)
	if !ok {
		return
	}
	room, found := server.rooms.Get(request.PathValue("id"))
	if !found {
		sendError(response, http.StatusNotFound, errors.New("комната не найдена"))
		return
	}
	if err := room.Join(user); err != nil {
		sendError(response, http.StatusConflict, err)
		return
	}
	room.Broadcast()
	sendJSON(response, http.StatusOK, map[string]any{"room": room.Snapshot(user.ID)})
}

func (server *Server) leaveRoom(response http.ResponseWriter, request *http.Request) {
	user, ok := server.requireUser(response, request)
	if !ok {
		return
	}
	if err := server.rooms.Leave(request.PathValue("id"), user.ID); err != nil {
		sendError(response, http.StatusConflict, err)
		return
	}
	sendJSON(response, http.StatusOK, map[string]bool{"ok": true})
}

func (server *Server) getRoom(response http.ResponseWriter, request *http.Request) {
	user, ok := server.requireUser(response, request)
	if !ok {
		return
	}
	room, found := server.rooms.Get(request.PathValue("id"))
	if !found {
		sendError(response, http.StatusNotFound, errors.New("комната не найдена"))
		return
	}
	room.mu.RLock()
	member := playerIndex(room.Players, user.ID) >= 0
	room.mu.RUnlock()
	if !member {
		sendError(response, http.StatusForbidden, errors.New("сначала присоединитесь к комнате"))
		return
	}
	sendJSON(response, http.StatusOK, map[string]any{"room": room.Snapshot(user.ID)})
}

func executeRoomAction(room *Room, userID string, action string) error {
	switch action {
	case "roll":
		return room.Roll(userID)
	case "property":
		return room.PropertyAction(userID)
	case "bail":
		return room.Bail(userID)
	case "auction_start":
		return room.StartAuction(userID)
	case "auction_decline":
		return room.DeclineAuction(userID)
	case "end_turn":
		return room.EndTurn(userID)
	default:
		return errors.New("неизвестное действие")
	}
}

func (server *Server) roomAction(response http.ResponseWriter, request *http.Request) {
	user, ok := server.requireUser(response, request)
	if !ok {
		return
	}
	room, found := server.rooms.Get(request.PathValue("id"))
	if !found {
		sendError(response, http.StatusNotFound, errors.New("комната не найдена"))
		return
	}
	if err := executeRoomAction(room, user.ID, request.PathValue("action")); err != nil {
		sendError(response, http.StatusConflict, err)
		return
	}
	room.Broadcast()
	sendJSON(response, http.StatusOK, map[string]any{"room": room.Snapshot(user.ID)})
}

func (server *Server) roomSocket(response http.ResponseWriter, request *http.Request) {
	user, ok := server.auth.User(request)
	if !ok {
		sendError(response, http.StatusUnauthorized, errors.New("нужно войти через Google"))
		return
	}
	room, found := server.rooms.Get(request.PathValue("id"))
	if !found {
		sendError(response, http.StatusNotFound, errors.New("комната не найдена"))
		return
	}
	room.mu.RLock()
	member := playerIndex(room.Players, user.ID) >= 0
	room.mu.RUnlock()
	if !member {
		sendError(response, http.StatusForbidden, errors.New("сначала присоединитесь к комнате"))
		return
	}
	connection, err := server.upgrader.Upgrade(response, request, nil)
	if err != nil {
		return
	}
	client := &WSClient{connection: connection, userID: user.ID}
	connection.SetReadLimit(16 << 10)
	connection.SetReadDeadline(time.Now().Add(70 * time.Second))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(70 * time.Second))
	})
	room.AddClient(client)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				client.mu.Lock()
				err := connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
				client.mu.Unlock()
				if err != nil {
					connection.Close()
					return
				}
			case <-done:
				return
			}
		}
	}()
	defer func() {
		close(done)
		room.RemoveClient(client)
		connection.Close()
	}()
	for {
		var command struct {
			Type   string `json:"type"`
			Amount int    `json:"amount"`
		}
		if err := connection.ReadJSON(&command); err != nil {
			return
		}
		var actionError error
		if command.Type == "auction_bid" {
			actionError = room.Bid(user.ID, command.Amount)
		} else {
			actionError = executeRoomAction(room, user.ID, command.Type)
		}
		if actionError != nil {
			client.Write(map[string]any{"type": "error", "error": actionError.Error()})
			continue
		}
		if command.Type != "auction_bid" {
			room.Broadcast()
		}
	}
}

func (server *Server) static(response http.ResponseWriter, request *http.Request) {
	path := request.URL.Path
	if path == "/" {
		path = "/index.html"
	} else if path == "/game" || path == "/game.html" {
		path = "/pages/game.html"
	}
	clean := filepath.Clean(strings.TrimPrefix(path, "/"))
	filePath := filepath.Join(server.publicRoot, clean)
	relation, err := filepath.Rel(server.publicRoot, filePath)
	if err != nil || strings.HasPrefix(relation, "..") || filepath.IsAbs(relation) {
		sendError(response, http.StatusNotFound, errors.New("not found"))
		return
	}
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		sendError(response, http.StatusNotFound, errors.New("not found"))
		return
	}
	if strings.HasSuffix(filePath, ".html") || strings.HasSuffix(filePath, ".js") || strings.HasSuffix(filePath, ".css") {
		response.Header().Set("Cache-Control", "no-store")
	}
	http.ServeFile(response, request, filePath)
}
