package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HotelService struct {
	baseURL string
	http    *http.Client
}

func NewHotelService(baseURL string) *HotelService {
	return &HotelService{baseURL: baseURL, http: &http.Client{Timeout: 90 * time.Second}}
}

func (service *HotelService) Countries(ctx context.Context) ([]CountryOption, error) {
	var payload struct {
		Countries []CountryOption `json:"countries"`
	}
	if err := service.get(ctx, "/countries", &payload); err != nil {
		return nil, err
	}
	return payload.Countries, nil
}

func (service *HotelService) Country(ctx context.Context, code string) (CountryConfig, error) {
	var country CountryConfig
	path := "/countries/" + url.PathEscape(strings.ToUpper(strings.TrimSpace(code)))
	if err := service.get(ctx, path, &country); err != nil {
		return country, err
	}
	return country, nil
}

func (service *HotelService) get(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, service.baseURL+path, nil)
	if err != nil {
		return err
	}
	response, err := service.http.Do(request)
	if err != nil {
		return fmt.Errorf("python hotel service unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("python hotel service returned %s: %s", response.Status, string(body))
	}
	return json.NewDecoder(io.LimitReader(response.Body, 20<<20)).Decode(target)
}

func (service *HotelService) Load(ctx context.Context, cities []string) (HotelPools, error) {
	var result HotelPools
	checkIn := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	checkOut := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	values := url.Values{"check_in": {checkIn}, "check_out": {checkOut}, "city": cities}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, service.baseURL+"/hotels/pools?"+values.Encode(), nil)
	if err != nil {
		return result, err
	}
	response, err := service.http.Do(request)
	if err != nil {
		return result, fmt.Errorf("python hotel service unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return result, fmt.Errorf("python hotel service returned %s: %s", response.Status, string(body))
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 20<<20)).Decode(&result); err != nil {
		return result, err
	}
	if len(result.Pools["1"]) < 12 {
		return result, fmt.Errorf("Tutu MCP returned only %d one-star hotels", len(result.Pools["1"]))
	}
	for level := 2; level <= 5; level++ {
		if len(result.Pools[fmt.Sprint(level)]) == 0 {
			return result, fmt.Errorf("Tutu MCP returned no hotels for level %d", level)
		}
	}
	return result, nil
}
