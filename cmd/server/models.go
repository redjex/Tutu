package main

import "time"

type User struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

type Session struct {
	User      User
	ExpiresAt time.Time
}

type Hotel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Stars         int      `json:"stars"`
	Rating        *float64 `json:"rating,omitempty"`
	ReviewCount   *int     `json:"review_count,omitempty"`
	Address       *string  `json:"address,omitempty"`
	City          string   `json:"city"`
	Photos        []string `json:"photos"`
	CheckoutURL   string   `json:"checkout_url"`
	PriceAmount   *float64 `json:"price_amount,omitempty"`
	PriceCurrency *string  `json:"price_currency,omitempty"`
}

type HotelPools struct {
	CheckIn  string             `json:"check_in"`
	CheckOut string             `json:"check_out"`
	Source   string             `json:"source"`
	Pools    map[string][]Hotel `json:"pools"`
}

type Tile struct {
	Type          string   `json:"type"`
	Name          string   `json:"name"`
	Icon          string   `json:"icon,omitempty"`
	Note          string   `json:"note,omitempty"`
	Color         string   `json:"color,omitempty"`
	Group         int      `json:"group"`
	PurchasePrice int      `json:"purchasePrice,omitempty"`
	Level         int      `json:"level,omitempty"`
	HotelID       string   `json:"id,omitempty"`
	Stars         int      `json:"stars,omitempty"`
	Rating        *float64 `json:"rating,omitempty"`
	ReviewCount   *int     `json:"review_count,omitempty"`
	Address       *string  `json:"address,omitempty"`
	City          string   `json:"city,omitempty"`
	Photos        []string `json:"photos,omitempty"`
	CheckoutURL   string   `json:"checkout_url,omitempty"`
	PriceAmount   *float64 `json:"price_amount,omitempty"`
	PriceCurrency *string  `json:"price_currency,omitempty"`
	Owner         string   `json:"owner,omitempty"`
	OwnerColor    string   `json:"ownerColor,omitempty"`
	Seed          int      `json:"-"`
}

type Player struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Picture    string `json:"picture"`
	Color      string `json:"color"`
	Cash       int    `json:"cash"`
	Position   int    `json:"position"`
	JailTurns  int    `json:"jailTurns"`
	Connected  bool   `json:"connected"`
	Bankrupt   bool   `json:"bankrupt"`
	LastAction int    `json:"-"`
}

type CountryOption struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type RoomSettings struct {
	Name        string `json:"name"`
	Visibility  string `json:"visibility"`
	Mode        string `json:"mode"`
	CountryCode string `json:"countryCode"`
	CountryName string `json:"countryName"`
	MaxPlayers  int    `json:"maxPlayers"`
}

type CreateRoomInput struct {
	Name        string `json:"name"`
	Visibility  string `json:"visibility"`
	Mode        string `json:"mode"`
	CountryCode string `json:"countryCode"`
	MaxPlayers  int    `json:"maxPlayers"`
}

type RoomSnapshot struct {
	ID                 string           `json:"id"`
	Status             string           `json:"status"`
	Players            []*Player        `json:"players"`
	Deck               []Tile           `json:"deck"`
	TurnPlayerID       string           `json:"turnPlayerId"`
	AwaitingActionID   string           `json:"awaitingActionId,omitempty"`
	AwaitingActionType string           `json:"awaitingActionType,omitempty"`
	YouID              string           `json:"youId"`
	Message            string           `json:"message"`
	LastDice           int              `json:"lastDice,omitempty"`
	Version            int64            `json:"version"`
	Source             string           `json:"source"`
	Settings           RoomSettings     `json:"settings"`
	Auction            *AuctionSnapshot `json:"auction,omitempty"`
}

type AuctionSnapshot struct {
	TileIndex       int       `json:"tileIndex"`
	SellerID        string    `json:"sellerId"`
	HighestBid      int       `json:"highestBid"`
	HighestBidderID string    `json:"highestBidderId,omitempty"`
	EndsAt          time.Time `json:"endsAt"`
	DeclinedIDs     []string  `json:"declinedIds,omitempty"`
}

type RoomSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Visibility  string `json:"visibility"`
	Mode        string `json:"mode"`
	CountryCode string `json:"countryCode"`
	CountryName string `json:"countryName"`
	Players     int    `json:"players"`
	Capacity    int    `json:"capacity"`
	HostName    string `json:"hostName"`
	CoverURL    string `json:"coverUrl"`
	CreatedAt   string `json:"createdAt"`
}
