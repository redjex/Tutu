package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestAuthenticatedRoomSnapshotAndWebSocket(t *testing.T) {
	auth := NewAuthStore("client-id", false)
	token, err := auth.Create(User{ID: "one", Name: "One"})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewRoomManager(NewHotelService("http://127.0.0.1:1"))
	room := testRoom()
	manager.rooms[room.ID] = room
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(NewServer("../../public", "client-id", "", "", auth, manager, logger, nil).Handler())
	defer server.Close()

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/rooms/ABC123", nil)
	request.AddCookie(&http.Cookie{Name: "tutu_session", Value: token})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %s", response.Status)
	}

	header := http.Header{}
	header.Add("Cookie", "tutu_session="+token)
	connection, response, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1)+"/api/rooms/ABC123/ws", header)
	if err != nil {
		if response != nil {
			response.Body.Close()
		}
		t.Fatal(err)
	}
	defer connection.Close()
	var payload struct {
		Type  string       `json:"type"`
		State RoomSnapshot `json:"state"`
	}
	if err := connection.ReadJSON(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Type != "state" || payload.State.YouID != "one" || len(payload.State.Deck) != 20 {
		value, _ := json.Marshal(payload)
		t.Fatalf("unexpected websocket payload: %s", value)
	}
	for _, tile := range payload.State.Deck {
		if tile.Type == "hotel" && len(tile.Photos) == 0 {
			t.Fatalf("hotel without photos: %+v", tile)
		}
	}
}

func TestCreatePrivateCountryRoom(t *testing.T) {
	auth := NewAuthStore("client-id", false)
	token, err := auth.Create(User{ID: "host", Name: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewRoomManager(NewHotelService("http://127.0.0.1:1"))
	manager.pools["TR"] = cachedPools{Pools: testPools(), Loaded: time.Now()}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(NewServer("../../public", "client-id", "", "", auth, manager, logger, nil).Handler())
	defer server.Close()

	body := strings.NewReader(`{"name":"Закрытая партия","visibility":"private","mode":"fast","countryCode":"TR","maxPlayers":3}`)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/rooms", body)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "tutu_session", Value: token})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		value, _ := io.ReadAll(response.Body)
		t.Fatalf("unexpected status %s: %s", response.Status, value)
	}
	var payload struct {
		Room RoomSnapshot `json:"room"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Room.Settings.Visibility != "private" || payload.Room.Settings.Mode != "fast" || payload.Room.Settings.CountryCode != "TR" || payload.Room.Settings.MaxPlayers != 3 {
		t.Fatalf("unexpected settings: %+v", payload.Room.Settings)
	}
	if len(manager.List()) != 0 {
		t.Fatal("private room leaked into public list")
	}
}
