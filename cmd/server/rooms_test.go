package main

import (
	"fmt"
	"testing"
	"time"
)

func testPools() HotelPools {
	pools := HotelPools{Source: "https://mcp.tutu.ru/mcp", Pools: map[string][]Hotel{}}
	for level := 1; level <= 5; level++ {
		key := fmt.Sprint(level)
		for index := 0; index < 20; index++ {
			pools.Pools[key] = append(pools.Pools[key], Hotel{ID: fmt.Sprintf("%d-%d", level, index), Name: fmt.Sprintf("Hotel %d-%d", level, index), Stars: level, City: "Москва", Photos: []string{"https://example.com/photo.jpg"}, CheckoutURL: "https://hotel.tutu.ru/offers/details"})
		}
	}
	return pools
}

func testRoom() *Room {
	room := &Room{ID: "ABC123", Status: "active", Pools: testPools(), Settings: RoomSettings{Name: "Test", Visibility: "public", Mode: "classic", CountryCode: "RU", CountryName: "Россия", MaxPlayers: 4}, CreatedAt: time.Now(), clients: map[*WSClient]struct{}{}}
	room.Deck = buildDeck(room.Pools)
	room.Players = []*Player{
		{ID: "one", Name: "One", Color: playerColors[0], Cash: 15000},
		{ID: "two", Name: "Two", Color: playerColors[1], Cash: 15000},
	}
	return room
}

func TestPropertyPurchaseAdvancesTurn(t *testing.T) {
	room := testRoom()
	room.Players[0].Position = 1
	room.AwaitingActionID = "one"
	room.AwaitingActionType = "buy"
	price := room.Deck[1].PurchasePrice
	if err := room.PropertyAction("one"); err != nil {
		t.Fatal(err)
	}
	if room.Deck[1].Owner != "one" || room.Players[0].Cash != 15000-price || room.Turn != 1 {
		t.Fatalf("unexpected purchase state: owner=%s cash=%d turn=%d", room.Deck[1].Owner, room.Players[0].Cash, room.Turn)
	}
}

func TestUpgradeUsesNextStarPool(t *testing.T) {
	room := testRoom()
	first := -1
	group := -1
	for index, tile := range room.Deck {
		if tile.Type == "hotel" {
			if first < 0 {
				first = index
				group = tile.Group
			}
			if tile.Group == group {
				room.Deck[index].Owner = "one"
				room.Deck[index].OwnerColor = playerColors[0]
			}
		}
	}
	room.Players[0].Position = first
	room.AwaitingActionID = "one"
	room.AwaitingActionType = "upgrade"
	if err := room.PropertyAction("one"); err != nil {
		t.Fatal(err)
	}
	if room.Deck[first].Stars != 2 || room.Deck[first].Level != 2 || room.Deck[first].CheckoutURL == "" {
		t.Fatalf("unexpected upgraded hotel: %+v", room.Deck[first])
	}
}

func TestRentTransfersCash(t *testing.T) {
	room := testRoom()
	index := 1
	room.Deck[index].Owner = "two"
	room.Players[0].Position = index
	beforeOne := room.Players[0].Cash
	beforeTwo := room.Players[1].Cash
	room.applyTile(room.Players[0])
	rent := room.Deck[index].PurchasePrice * room.Deck[index].Level / 5
	if room.Players[0].Cash != beforeOne-rent || room.Players[1].Cash != beforeTwo+rent {
		t.Fatalf("rent was not transferred: one=%d two=%d", room.Players[0].Cash, room.Players[1].Cash)
	}
}

func TestLeaveRemovesPlayerAndKeepsRoom(t *testing.T) {
	manager := NewRoomManager(NewHotelService("http://127.0.0.1:1"))
	room := testRoom()
	room.Status = "waiting"
	room.HostID = "one"
	manager.rooms[room.ID] = room
	if err := manager.Leave(room.ID, "one"); err != nil {
		t.Fatal(err)
	}
	remaining, ok := manager.Get(room.ID)
	if !ok || len(remaining.Players) != 1 || remaining.Players[0].ID != "two" || remaining.HostID != "two" {
		t.Fatalf("unexpected room after leave: %+v", remaining)
	}
}

func TestActiveLeaveMarksPlayerLost(t *testing.T) {
	room := testRoom()
	room.HostID = "one"
	if err := room.Leave("one"); err != nil {
		t.Fatal(err)
	}
	if len(room.Players) != 2 || !room.Players[0].Bankrupt || room.Status != "finished" {
		t.Fatalf("unexpected active leave state: players=%d bankrupt=%v status=%s", len(room.Players), room.Players[0].Bankrupt, room.Status)
	}
	if room.Message != "Two победил" {
		t.Fatalf("unexpected winner message: %s", room.Message)
	}
}

func TestRoomSettingsValidation(t *testing.T) {
	user := User{Name: "One"}
	settings, country, err := validateRoomSettings(CreateRoomInput{Name: "  Friends  ", Visibility: "private", Mode: "fast", CountryCode: "TR", MaxPlayers: 3}, user)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Name != "Friends" || settings.Visibility != "private" || settings.Mode != "fast" || settings.CountryName != "Турция" || settings.MaxPlayers != 3 || len(country.Cities) != 2 {
		t.Fatalf("unexpected settings: %+v %+v", settings, country)
	}
	if _, _, err := validateRoomSettings(CreateRoomInput{CountryCode: "XX"}, user); err == nil {
		t.Fatal("unsupported country was accepted")
	}
}

func TestPrivateRoomsAreNotListed(t *testing.T) {
	manager := NewRoomManager(NewHotelService("http://127.0.0.1:1"))
	publicRoom := testRoom()
	publicRoom.Status = "waiting"
	privateRoom := testRoom()
	privateRoom.ID = "PRIVATE"
	privateRoom.Settings.Visibility = "private"
	manager.rooms[publicRoom.ID] = publicRoom
	manager.rooms[privateRoom.ID] = privateRoom
	rooms := manager.List()
	if len(rooms) != 1 || rooms[0].ID != publicRoom.ID {
		t.Fatalf("unexpected public rooms: %+v", rooms)
	}
}

func TestFastModeStartingCash(t *testing.T) {
	room := testRoom()
	room.Status = "waiting"
	room.Settings.Mode = "fast"
	room.Players = room.Players[:1]
	if err := room.Join(User{ID: "fast", Name: "Fast"}); err != nil {
		t.Fatal(err)
	}
	if room.Players[1].Cash != 20000 {
		t.Fatalf("unexpected fast mode cash: %d", room.Players[1].Cash)
	}
}

func TestRoomStartsOnlyAtCapacity(t *testing.T) {
	room := testRoom()
	room.Status = "waiting"
	room.Settings.MaxPlayers = 4
	room.Players = room.Players[:1]
	for _, id := range []string{"two", "three"} {
		if err := room.Join(User{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
		if room.Status != "waiting" {
			t.Fatalf("room started with %d players", len(room.Players))
		}
	}
	if err := room.Join(User{ID: "four", Name: "four"}); err != nil {
		t.Fatal(err)
	}
	if room.Status != "active" {
		t.Fatalf("room did not start at capacity: %s", room.Status)
	}
}

func TestActiveRoomRejectsNewPlayers(t *testing.T) {
	room := testRoom()
	room.Status = "active"
	if err := room.Join(User{ID: "three", Name: "Three"}); err == nil {
		t.Fatal("active room accepted a new player")
	}
	if len(room.Players) != 2 {
		t.Fatalf("active room changed player count: %d", len(room.Players))
	}
}
