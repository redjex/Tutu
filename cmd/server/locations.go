package main

import (
	"errors"
	"sort"
	"strings"
)

type CountryConfig struct {
	Code   string
	Name   string
	Cities []string
}

var countryCatalog = map[string]CountryConfig{
	"RU": {Code: "RU", Name: "Россия", Cities: []string{"Москва", "Санкт-Петербург"}},
	"TR": {Code: "TR", Name: "Турция", Cities: []string{"Стамбул", "Анталья"}},
	"AE": {Code: "AE", Name: "ОАЭ", Cities: []string{"Дубай", "Абу-Даби"}},
	"TH": {Code: "TH", Name: "Таиланд", Cities: []string{"Бангкок", "Пхукет"}},
	"IT": {Code: "IT", Name: "Италия", Cities: []string{"Рим", "Милан"}},
}

func countries() []CountryOption {
	result := make([]CountryOption, 0, len(countryCatalog))
	for _, country := range countryCatalog {
		result = append(result, CountryOption{Code: country.Code, Name: country.Name})
	}
	sort.Slice(result, func(left int, right int) bool { return result[left].Name < result[right].Name })
	return result
}

func validateRoomSettings(input CreateRoomInput, user User) (RoomSettings, CountryConfig, error) {
	code := strings.ToUpper(strings.TrimSpace(input.CountryCode))
	if code == "" {
		code = "RU"
	}
	country, ok := countryCatalog[code]
	if !ok {
		return RoomSettings{}, CountryConfig{}, errors.New("страна не поддерживается")
	}
	settings, err := validateRoomSettingsForCountry(input, user, country)
	return settings, country, err
}

func validateRoomSettingsForCountry(input CreateRoomInput, user User, country CountryConfig) (RoomSettings, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "Лобби " + user.Name
	}
	if len([]rune(name)) > 40 {
		return RoomSettings{}, errors.New("название лобби не должно превышать 40 символов")
	}
	visibility := strings.ToLower(strings.TrimSpace(input.Visibility))
	if visibility == "" {
		visibility = "public"
	}
	if visibility != "public" && visibility != "private" {
		return RoomSettings{}, errors.New("видимость должна быть public или private")
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = "classic"
	}
	if mode != "classic" && mode != "fast" {
		return RoomSettings{}, errors.New("режим должен быть classic или fast")
	}
	maxPlayers := input.MaxPlayers
	if maxPlayers == 0 {
		maxPlayers = 4
	}
	if maxPlayers < 2 || maxPlayers > 4 {
		return RoomSettings{}, errors.New("в лобби может быть от 2 до 4 игроков")
	}
	settings := RoomSettings{Name: name, Visibility: visibility, Mode: mode, CountryCode: country.Code, CountryName: country.Name, MaxPlayers: maxPlayers}
	return settings, nil
}
