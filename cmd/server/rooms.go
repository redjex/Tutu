package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var playerColors = []string{"#313b80", "#d85b50", "#57945f", "#8062a8"}
var groupColors = []string{"#7889d7", "#78b99b", "#efad69", "#d98787", "#9b86cb", "#65a8be"}

var specialTiles = map[int]Tile{
	0:  {Type: "start", Name: "Старт", Icon: "/assets/images/fly.svg", Note: "Получите $2 000"},
	3:  {Type: "chance", Name: "Шанс", Icon: "/assets/images/change.svg", Note: "Случайное событие"},
	5:  {Type: "perk", Name: "Плюшка", Icon: "/assets/images/star.svg", Note: "Получите бонус"},
	8:  {Type: "cafe", Name: "Кафе", Icon: "/assets/images/market.svg", Note: "Перерыв и бонус $300"},
	10: {Type: "jail", Name: "Тюрьма", Icon: "/assets/images/prizen.svg", Note: "Пропуск двух ходов"},
	13: {Type: "cafe", Name: "Кафе", Icon: "/assets/images/market.svg", Note: "Перерыв и бонус $300"},
	15: {Type: "tax", Name: "Налог", Icon: "/assets/images/fee.svg", Note: "Заплатите $1 000"},
	18: {Type: "chance", Name: "Шанс", Icon: "/assets/images/change.svg", Note: "Случайное событие"},
}

type WSClient struct {
	connection *websocket.Conn
	userID     string
	mu         sync.Mutex
}

func (client *WSClient) Write(value any) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.connection.WriteJSON(value)
}

type Room struct {
	mu                 sync.RWMutex
	ID                 string
	HostID             string
	Status             string
	Players            []*Player
	Deck               []Tile
	Turn               int
	AwaitingActionID   string
	AwaitingActionType string
	Message            string
	LastDice           int
	Version            int64
	Pools              HotelPools
	Settings           RoomSettings
	Auction            *AuctionState
	CreatedAt          time.Time
	clients            map[*WSClient]struct{}
}

type AuctionState struct {
	TileIndex       int
	SellerID        string
	HighestBid      int
	HighestBidderID string
	EndsAt          time.Time
	DeclinedIDs     map[string]bool
}

type cachedPools struct {
	Pools  HotelPools
	Loaded time.Time
}

type RoomManager struct {
	mu      sync.RWMutex
	rooms   map[string]*Room
	hotels  *HotelService
	loading sync.Mutex
	pools   map[string]cachedPools
}

func NewRoomManager(hotels *HotelService) *RoomManager {
	return &RoomManager{rooms: map[string]*Room{}, hotels: hotels, pools: map[string]cachedPools{}}
}

func secureInt(limit int) int {
	if limit <= 1 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return int(time.Now().UnixNano() % int64(limit))
	}
	return int(value.Int64())
}

func roomCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var value strings.Builder
	for range 6 {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		value.WriteByte(alphabet[index.Int64()])
	}
	return value.String(), nil
}

func applyHotel(tile *Tile, hotel Hotel) {
	tile.Name = hotel.Name
	tile.HotelID = hotel.ID
	tile.Stars = hotel.Stars
	tile.Level = hotel.Stars
	tile.Rating = hotel.Rating
	tile.ReviewCount = hotel.ReviewCount
	tile.Address = hotel.Address
	tile.City = hotel.City
	tile.Photos = append([]string(nil), hotel.Photos...)
	tile.CheckoutURL = hotel.CheckoutURL
	tile.PriceAmount = hotel.PriceAmount
	tile.PriceCurrency = hotel.PriceCurrency
}

func buildDeck(pools HotelPools) []Tile {
	hotels := append([]Hotel(nil), pools.Pools["1"]...)
	for index := len(hotels) - 1; index > 0; index-- {
		swap := secureInt(index + 1)
		hotels[index], hotels[swap] = hotels[swap], hotels[index]
	}
	deck := make([]Tile, 20)
	hotelIndex := 0
	for index := range deck {
		if special, ok := specialTiles[index]; ok {
			deck[index] = special
			continue
		}
		group := hotelIndex % len(groupColors)
		tile := Tile{Type: "hotel", Color: groupColors[group], Group: group, PurchasePrice: 1000 + index*350, Level: 1, Seed: hotelIndex + secureInt(len(hotels))}
		applyHotel(&tile, hotels[hotelIndex%len(hotels)])
		deck[index] = tile
		hotelIndex++
	}
	return deck
}

func (manager *RoomManager) loadPools(ctx context.Context, country CountryConfig) (HotelPools, error) {
	manager.mu.RLock()
	cached, exists := manager.pools[country.Code]
	if exists && time.Since(cached.Loaded) < 15*time.Minute {
		result := cached.Pools
		manager.mu.RUnlock()
		return result, nil
	}
	manager.mu.RUnlock()
	manager.loading.Lock()
	defer manager.loading.Unlock()
	manager.mu.RLock()
	cached, exists = manager.pools[country.Code]
	if exists && time.Since(cached.Loaded) < 15*time.Minute {
		result := cached.Pools
		manager.mu.RUnlock()
		return result, nil
	}
	manager.mu.RUnlock()
	result, err := manager.hotels.Load(ctx, country.Cities)
	if err != nil {
		return result, err
	}
	manager.mu.Lock()
	manager.pools[country.Code] = cachedPools{Pools: result, Loaded: time.Now()}
	manager.mu.Unlock()
	return result, nil
}

func (manager *RoomManager) Create(ctx context.Context, user User, input CreateRoomInput) (*Room, error) {
	countryCode := strings.ToUpper(strings.TrimSpace(input.CountryCode))
	if countryCode == "" {
		countryCode = "RU"
	}
	country, exists := countryCatalog[countryCode]
	var err error
	if !exists {
		country, err = manager.hotels.Country(ctx, countryCode)
		if err != nil {
			return nil, err
		}
	}
	settings, err := validateRoomSettingsForCountry(input, user, country)
	if err != nil {
		return nil, err
	}
	pools, err := manager.loadPools(ctx, country)
	if err != nil {
		return nil, err
	}
	var code string
	for range 10 {
		code, err = roomCode()
		if err != nil {
			return nil, err
		}
		manager.mu.RLock()
		_, exists := manager.rooms[code]
		manager.mu.RUnlock()
		if !exists {
			break
		}
	}
	room := &Room{ID: code, HostID: user.ID, Status: "waiting", Deck: buildDeck(pools), Pools: pools, Settings: settings, CreatedAt: time.Now(), clients: map[*WSClient]struct{}{}, Message: "Ожидаем друзей"}
	room.Players = []*Player{{ID: user.ID, Name: user.Name, Picture: user.Picture, Color: playerColors[0], Cash: startingCash(settings.Mode), Connected: true}}
	manager.mu.Lock()
	manager.rooms[code] = room
	manager.mu.Unlock()
	return room, nil
}

func (manager *RoomManager) Get(id string) (*Room, bool) {
	manager.mu.RLock()
	room, ok := manager.rooms[strings.ToUpper(id)]
	manager.mu.RUnlock()
	return room, ok
}

func (manager *RoomManager) Leave(id string, userID string) error {
	room, ok := manager.Get(id)
	if !ok {
		return errors.New("комната не найдена")
	}
	if err := room.Leave(userID); err != nil {
		return err
	}
	room.mu.RLock()
	empty := len(room.Players) == 0
	room.mu.RUnlock()
	if empty {
		manager.mu.Lock()
		delete(manager.rooms, room.ID)
		manager.mu.Unlock()
	}
	return nil
}

func (manager *RoomManager) List() []RoomSummary {
	manager.mu.RLock()
	rooms := make([]*Room, 0, len(manager.rooms))
	for _, room := range manager.rooms {
		rooms = append(rooms, room)
	}
	manager.mu.RUnlock()
	result := make([]RoomSummary, 0, len(rooms))
	for _, room := range rooms {
		room.mu.RLock()
		if room.Status == "waiting" && room.Settings.Visibility == "public" && len(room.Players) > 0 && len(room.Players) < room.Settings.MaxPlayers {
			coverURL := ""
			for _, tile := range room.Deck {
				if tile.Type == "hotel" && len(tile.Photos) > 0 {
					coverURL = tile.Photos[0]
					break
				}
			}
			result = append(result, RoomSummary{ID: room.ID, Name: room.Settings.Name, Status: room.Status, Visibility: room.Settings.Visibility, Mode: room.Settings.Mode, CountryCode: room.Settings.CountryCode, CountryName: room.Settings.CountryName, Players: len(room.Players), Capacity: room.Settings.MaxPlayers, HostName: room.Players[0].Name, CoverURL: coverURL, CreatedAt: room.CreatedAt.UTC().Format(time.RFC3339)})
		}
		room.mu.RUnlock()
	}
	return result
}

func playerIndex(players []*Player, id string) int {
	for index, player := range players {
		if player.ID == id {
			return index
		}
	}
	return -1
}

func startingCash(mode string) int {
	if mode == "fast" {
		return 20000
	}
	return 15000
}

func (room *Room) Join(user User) error {
	room.mu.Lock()
	defer room.mu.Unlock()
	if index := playerIndex(room.Players, user.ID); index >= 0 {
		room.Players[index].Connected = true
		return nil
	}
	if room.Status == "finished" {
		return errors.New("комната недоступна")
	}
	if room.Status == "active" {
		return errors.New("партия уже началась")
	}
	if len(room.Players) >= room.Settings.MaxPlayers {
		return errors.New("комната полна")
	}
	room.Players = append(room.Players, &Player{ID: user.ID, Name: user.Name, Picture: user.Picture, Color: playerColors[len(room.Players)], Cash: startingCash(room.Settings.Mode), Connected: true})
	if len(room.Players) >= room.Settings.MaxPlayers && room.Status == "waiting" {
		room.Status = "active"
		room.Message = fmt.Sprintf("Ход игрока %s", room.Players[room.Turn].Name)
	}
	room.Version++
	return nil
}

func (room *Room) Leave(userID string) error {
	room.mu.Lock()
	index := playerIndex(room.Players, userID)
	if index < 0 {
		room.mu.Unlock()
		return errors.New("игрок не состоит в комнате")
	}
	if room.Status == "active" {
		player := room.Players[index]
		wasCurrent := room.Turn == index
		room.releaseProperties(userID)
		player.Bankrupt = true
		player.Connected = false
		if room.AwaitingActionID == userID {
			room.AwaitingActionID = ""
			room.AwaitingActionType = ""
		}
		if wasCurrent {
			room.advanceTurn()
		} else {
			room.finishIfOneActive()
		}
		room.Version++
		clients := make([]*WSClient, 0)
		for client := range room.clients {
			if client.userID == userID {
				clients = append(clients, client)
				delete(room.clients, client)
			}
		}
		room.mu.Unlock()
		for _, client := range clients {
			client.connection.Close()
		}
		room.Broadcast()
		return nil
	}
	room.releaseProperties(userID)
	wasCurrent := room.Turn == index
	room.Players = append(room.Players[:index], room.Players[index+1:]...)
	if index < room.Turn {
		room.Turn--
	}
	if room.Turn >= len(room.Players) {
		room.Turn = 0
	}
	if room.AwaitingActionID == userID {
		room.AwaitingActionID = ""
		room.AwaitingActionType = ""
	}
	if len(room.Players) == 0 {
		room.Status = "finished"
		room.Message = "Комната закрыта"
	} else if room.Status == "active" && len(room.Players) < 2 {
		room.Status = "finished"
		room.Message = fmt.Sprintf("%s победил", room.Players[0].Name)
	} else if wasCurrent && room.Status == "active" {
		room.Message = fmt.Sprintf("Ход игрока %s", room.Players[room.Turn].Name)
	}
	if room.HostID == userID && len(room.Players) > 0 {
		room.HostID = room.Players[0].ID
	}
	clients := make([]*WSClient, 0)
	for client := range room.clients {
		if client.userID == userID {
			clients = append(clients, client)
			delete(room.clients, client)
		}
	}
	room.Version++
	room.mu.Unlock()
	for _, client := range clients {
		client.connection.Close()
	}
	room.Broadcast()
	return nil
}

func (room *Room) player(id string) (*Player, int) {
	index := playerIndex(room.Players, id)
	if index < 0 {
		return nil, -1
	}
	return room.Players[index], index
}

func (room *Room) currentPlayer() *Player {
	if len(room.Players) == 0 || room.Turn >= len(room.Players) {
		return nil
	}
	return room.Players[room.Turn]
}

func (room *Room) advanceTurn() {
	room.AwaitingActionID = ""
	room.AwaitingActionType = ""
	active := 0
	for _, player := range room.Players {
		if !player.Bankrupt {
			active++
		}
	}
	if room.finishIfOneActive() {
		return
	}
	for range len(room.Players) {
		room.Turn = (room.Turn + 1) % len(room.Players)
		if !room.Players[room.Turn].Bankrupt {
			break
		}
	}
}

func (room *Room) finishIfOneActive() bool {
	active := 0
	winner := ""
	for _, player := range room.Players {
		if !player.Bankrupt {
			active++
			winner = player.Name
		}
	}
	if active <= 1 && len(room.Players) >= 2 {
		room.Status = "finished"
		if winner != "" {
			room.Message = fmt.Sprintf("%s победил", winner)
		}
		return true
	}
	return false
}

func (room *Room) ownsGroup(userID string, group int) bool {
	found := false
	for _, tile := range room.Deck {
		if tile.Type == "hotel" && tile.Group == group {
			found = true
			if tile.Owner != userID {
				return false
			}
		}
	}
	return found
}

func upgradeCost(tile Tile) int {
	return int(float64(tile.PurchasePrice) * (0.3 + float64(tile.Level)*0.1))
}

func (room *Room) canUpgrade(player *Player, tile Tile) bool {
	return tile.Owner == player.ID && tile.Level < 5 && room.ownsGroup(player.ID, tile.Group) && player.Cash >= upgradeCost(tile)
}

func (room *Room) hotelForLevel(tile Tile, level int) (Hotel, bool) {
	pool := room.Pools.Pools[fmt.Sprint(level)]
	if len(pool) == 0 {
		return Hotel{}, false
	}
	start := (tile.Seed + level) % len(pool)
	for offset := range len(pool) {
		hotel := pool[(start+offset)%len(pool)]
		if hotel.ID != tile.HotelID {
			return hotel, true
		}
	}
	return Hotel{}, false
}

func (room *Room) releaseProperties(userID string) {
	for index := range room.Deck {
		if room.Deck[index].Owner == userID {
			room.Deck[index].Owner = ""
			room.Deck[index].OwnerColor = ""
		}
	}
}

func (room *Room) charge(player *Player, amount int) int {
	if amount > player.Cash {
		paid := player.Cash
		player.Cash = 0
		player.Bankrupt = true
		room.releaseProperties(player.ID)
		return paid
	}
	player.Cash -= amount
	return amount
}

func (room *Room) applyTile(player *Player) bool {
	tile := &room.Deck[player.Position]
	switch tile.Type {
	case "start":
		room.Message = fmt.Sprintf("%s на старте", player.Name)
	case "jail":
		player.JailTurns = 2
		room.Message = fmt.Sprintf("%s попал в тюрьму", player.Name)
	case "chance":
		values := []int{500, -500, 1000, -300}
		amount := values[secureInt(len(values))]
		if amount >= 0 {
			player.Cash += amount
		} else {
			room.charge(player, -amount)
		}
		room.Message = fmt.Sprintf("Шанс: баланс %s изменился на %d", player.Name, amount)
	case "perk":
		values := []int{400, 700, 1000}
		amount := values[secureInt(len(values))]
		player.Cash += amount
		room.Message = fmt.Sprintf("Плюшка для %s: +%d", player.Name, amount)
	case "cafe":
		player.Cash += 300
		room.Message = fmt.Sprintf("%s получил в кафе 300", player.Name)
	case "tax":
		paid := room.charge(player, 1000)
		room.Message = fmt.Sprintf("%s заплатил налог %d", player.Name, paid)
	case "hotel":
		if tile.Owner == "" {
			room.AwaitingActionID = player.ID
			room.AwaitingActionType = "buy"
			room.Message = fmt.Sprintf("%s может купить %s", player.Name, tile.Name)
			return true
		}
		if tile.Owner == player.ID {
			if room.canUpgrade(player, *tile) {
				room.AwaitingActionID = player.ID
				room.AwaitingActionType = "upgrade"
				room.Message = fmt.Sprintf("%s может улучшить %s", player.Name, tile.Name)
				return true
			}
			room.Message = fmt.Sprintf("%s вернулся в %s", player.Name, tile.Name)
			break
		}
		owner, _ := room.player(tile.Owner)
		rent := tile.PurchasePrice * tile.Level / 5
		paid := room.charge(player, rent)
		if owner != nil {
			owner.Cash += paid
			room.Message = fmt.Sprintf("%s заплатил %s %d", player.Name, owner.Name, paid)
		}
	}
	return false
}

func (room *Room) Roll(userID string) error {
	room.mu.Lock()
	defer room.mu.Unlock()
	if room.Status != "active" {
		return errors.New("партия ещё не началась")
	}
	player := room.currentPlayer()
	if player == nil || player.ID != userID {
		return errors.New("сейчас ход другого игрока")
	}
	if room.AwaitingActionID != "" {
		return errors.New("сначала купите, улучшите или завершите ход")
	}
	if player.JailTurns > 0 {
		player.JailTurns--
		room.LastDice = 0
		room.Message = fmt.Sprintf("%s пропускает ход в тюрьме", player.Name)
		room.advanceTurn()
		room.Version++
		return nil
	}
	dice := secureInt(6) + 1
	room.LastDice = dice
	oldPosition := player.Position
	player.Position = (player.Position + dice) % len(room.Deck)
	if oldPosition+dice >= len(room.Deck) {
		player.Cash += 2000
	}
	awaiting := room.applyTile(player)
	if !awaiting {
		room.advanceTurn()
	}
	room.Version++
	return nil
}

func (room *Room) StartAuction(userID string) error {
	room.mu.Lock()
	defer room.mu.Unlock()
	if room.Status != "active" || room.Auction != nil || room.AwaitingActionID != userID || room.AwaitingActionType != "buy" {
		return errors.New("аукцион сейчас недоступен")
	}
	player := room.currentPlayer()
	if player == nil || player.ID != userID {
		return errors.New("аукцион может начать только игрок, которому выпал отель")
	}
	room.Auction = &AuctionState{TileIndex: player.Position, SellerID: userID, EndsAt: time.Now().Add(10 * time.Second), DeclinedIDs: map[string]bool{}}
	room.AwaitingActionType = "auction"
	room.Message = fmt.Sprintf("%s выставил отель на аукцион", player.Name)
	room.Version++
	endsAt := room.Auction.EndsAt
	go func() {
		time.Sleep(time.Until(endsAt))
		room.FinishAuction()
	}()
	return nil
}

func (room *Room) Bid(userID string, amount int) error {
	room.mu.Lock()
	if room.Auction == nil {
		room.mu.Unlock()
		return errors.New("аукцион не идёт")
	}
	if time.Now().After(room.Auction.EndsAt) {
		room.mu.Unlock()
		room.FinishAuction()
		return errors.New("аукцион уже завершён")
	}
	if userID == room.Auction.SellerID || room.Auction.DeclinedIDs[userID] || amount < 100 || amount%100 != 0 || amount <= room.Auction.HighestBid {
		room.mu.Unlock()
		return errors.New("ставка должна быть выше текущей минимум на 100")
	}
	player, _ := room.player(userID)
	if player == nil || player.Bankrupt || player.Cash < amount {
		room.mu.Unlock()
		return errors.New("недостаточно денег для ставки")
	}
	room.Auction.HighestBid = amount
	room.Auction.HighestBidderID = userID
	room.Message = fmt.Sprintf("%s поставил %d", player.Name, amount)
	room.Version++
	room.mu.Unlock()
	room.Broadcast()
	return nil
}

func (room *Room) DeclineAuction(userID string) error {
	room.mu.Lock()
	defer room.mu.Unlock()
	if room.Auction == nil || userID == room.Auction.SellerID {
		return errors.New("отказ от аукциона сейчас недоступен")
	}
	room.Auction.DeclinedIDs[userID] = true
	room.Version++
	return nil
}

func (room *Room) FinishAuction() {
	room.mu.Lock()
	if room.Auction == nil {
		room.mu.Unlock()
		return
	}
	auction := room.Auction
	tile := &room.Deck[auction.TileIndex]
	if auction.HighestBidderID != "" {
		winner, _ := room.player(auction.HighestBidderID)
		if winner != nil && winner.Cash >= auction.HighestBid {
			winner.Cash -= auction.HighestBid
			tile.Owner = winner.ID
			tile.OwnerColor = winner.Color
			room.Message = fmt.Sprintf("%s выиграл аукцион за %d", winner.Name, auction.HighestBid)
		}
	} else {
		room.Message = "Аукцион завершён без ставок"
	}
	room.Auction = nil
	room.AwaitingActionID = ""
	room.AwaitingActionType = ""
	room.advanceTurn()
	room.Version++
	room.mu.Unlock()
	room.Broadcast()
}

func (room *Room) EndTurn(userID string) error {
	room.mu.Lock()
	defer room.mu.Unlock()
	if room.AwaitingActionID != userID || room.currentPlayer() == nil || room.currentPlayer().ID != userID {
		return errors.New("завершать ход сейчас нельзя")
	}
	room.Message = fmt.Sprintf("%s завершил ход", room.currentPlayer().Name)
	room.advanceTurn()
	room.Version++
	return nil
}

func (room *Room) PropertyAction(userID string) error {
	room.mu.Lock()
	defer room.mu.Unlock()
	player := room.currentPlayer()
	if player == nil || player.ID != userID || room.AwaitingActionID != userID {
		return errors.New("действие с отелем сейчас недоступно")
	}
	tile := &room.Deck[player.Position]
	if room.AwaitingActionType == "buy" {
		if tile.Owner != "" || player.Cash < tile.PurchasePrice {
			return errors.New("отель нельзя купить")
		}
		player.Cash -= tile.PurchasePrice
		tile.Owner = player.ID
		tile.OwnerColor = player.Color
		room.Message = fmt.Sprintf("%s купил %s", player.Name, tile.Name)
	} else if room.AwaitingActionType == "upgrade" {
		if !room.canUpgrade(player, *tile) {
			return errors.New("отель нельзя улучшить")
		}
		cost := upgradeCost(*tile)
		hotel, ok := room.hotelForLevel(*tile, tile.Level+1)
		if !ok {
			return errors.New("Tutu MCP не вернул отель следующего уровня")
		}
		player.Cash -= cost
		applyHotel(tile, hotel)
		tile.Owner = player.ID
		tile.OwnerColor = player.Color
		room.Message = fmt.Sprintf("%s улучшил отель до %d звёзд", player.Name, tile.Level)
	} else {
		return errors.New("неизвестное действие")
	}
	room.advanceTurn()
	room.Version++
	return nil
}

func (room *Room) Bail(userID string) error {
	room.mu.Lock()
	defer room.mu.Unlock()
	player := room.currentPlayer()
	if player == nil || player.ID != userID || player.JailTurns == 0 || player.Cash < 2000 {
		return errors.New("выйти из тюрьмы сейчас нельзя")
	}
	player.Cash -= 2000
	player.JailTurns = 0
	room.Message = fmt.Sprintf("%s вышел из тюрьмы за 2000", player.Name)
	room.Version++
	return nil
}

func (room *Room) snapshotLocked(userID string) RoomSnapshot {
	players := make([]*Player, len(room.Players))
	for index, player := range room.Players {
		copyValue := *player
		players[index] = &copyValue
	}
	deck := append([]Tile(nil), room.Deck...)
	turnPlayerID := ""
	if current := room.currentPlayer(); current != nil && room.Status == "active" {
		turnPlayerID = current.ID
	}
	var auction *AuctionSnapshot
	if room.Auction != nil {
		value := *room.Auction
		declined := make([]string, 0, len(value.DeclinedIDs))
		for id := range value.DeclinedIDs {
			declined = append(declined, id)
		}
		auction = &AuctionSnapshot{TileIndex: value.TileIndex, SellerID: value.SellerID, HighestBid: value.HighestBid, HighestBidderID: value.HighestBidderID, EndsAt: value.EndsAt, DeclinedIDs: declined}
	}
	return RoomSnapshot{ID: room.ID, Status: room.Status, Players: players, Deck: deck, TurnPlayerID: turnPlayerID, AwaitingActionID: room.AwaitingActionID, AwaitingActionType: room.AwaitingActionType, YouID: userID, Message: room.Message, LastDice: room.LastDice, Version: room.Version, Source: room.Pools.Source, Settings: room.Settings, Auction: auction}
}

func (room *Room) Snapshot(userID string) RoomSnapshot {
	room.mu.RLock()
	defer room.mu.RUnlock()
	return room.snapshotLocked(userID)
}

func (room *Room) AddClient(client *WSClient) {
	room.mu.Lock()
	room.clients[client] = struct{}{}
	if player, _ := room.player(client.userID); player != nil {
		player.Connected = true
	}
	room.Version++
	room.mu.Unlock()
	room.Broadcast()
}

func (room *Room) RemoveClient(client *WSClient) {
	room.mu.Lock()
	delete(room.clients, client)
	connected := false
	for existing := range room.clients {
		if existing.userID == client.userID {
			connected = true
			break
		}
	}
	if player, _ := room.player(client.userID); player != nil {
		player.Connected = connected
	}
	room.Version++
	room.mu.Unlock()
	room.Broadcast()
}

func (room *Room) Broadcast() {
	room.mu.RLock()
	clients := make([]*WSClient, 0, len(room.clients))
	snapshots := make([]RoomSnapshot, 0, len(room.clients))
	for client := range room.clients {
		clients = append(clients, client)
		snapshots = append(snapshots, room.snapshotLocked(client.userID))
	}
	room.mu.RUnlock()
	for index, client := range clients {
		if err := client.Write(map[string]any{"type": "state", "state": snapshots[index]}); err != nil {
			client.connection.Close()
		}
	}
}
