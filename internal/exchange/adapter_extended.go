package exchange

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	extendedBaseURL         = "https://api.starknet.extended.exchange/api/v1"
	extendedMarketsURL      = extendedBaseURL + "/info/markets"
	extendedFundingInterval = 3600 // 1 hour in seconds
)

// ExtendedAdapter implements REST-based data fetching for Extended
type ExtendedAdapter struct {
	code   string
	client *http.Client
}

type ExtendedMarketsResponse struct {
	Status string           `json:"status"`
	Data   []ExtendedMarket `json:"data"`
}

type ExtendedMarket struct {
	Name        string              `json:"name"`
	Type        string              `json:"type"`
	AssetName   string              `json:"assetName"`
	Active      bool                `json:"active"`
	Status      string              `json:"status"`
	MarketStats ExtendedMarketStats `json:"marketStats"`
}

type ExtendedMarketStats struct {
	DailyVolume      string `json:"dailyVolume"`
	DailyVolumeBase  string `json:"dailyVolumeBase"`
	LastPrice        string `json:"lastPrice"`
	AskPrice         string `json:"askPrice"`
	BidPrice         string `json:"bidPrice"`
	MarkPrice        string `json:"markPrice"`
	IndexPrice       string `json:"indexPrice"`
	FundingRate      string `json:"fundingRate"`
	OpenInterest     string `json:"openInterest"`
	OpenInterestBase string `json:"openInterestBase"`
}

type ExtendedFundingData struct {
	Symbol      string
	BaseAsset   string
	FundingRate string
	MarkPrice   string
	IndexPrice  string
	Volume24h   string
	OI          string
	ObservedAt  time.Time
}

func NewExtendedAdapter() *ExtendedAdapter {
	return &ExtendedAdapter{
		code:   "extended",
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *ExtendedAdapter) GetVenueCode() string {
	return a.code
}

func (a *ExtendedAdapter) GetFundingInterval() int {
	return extendedFundingInterval
}

// FetchAllMarkets fetches all markets with funding data
func (a *ExtendedAdapter) FetchAllMarkets(ctx context.Context) ([]ExtendedFundingData, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", extendedMarketsURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "ArbitragePlatform/1.0")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result ExtendedMarketsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var markets []ExtendedFundingData
	now := time.Now()

	for _, m := range result.Data {
		if m.Type != "PERPETUAL" || !m.Active {
			continue
		}

		market := ExtendedFundingData{
			Symbol:      m.Name,
			BaseAsset:   m.AssetName,
			FundingRate: m.MarketStats.FundingRate,
			MarkPrice:   m.MarketStats.MarkPrice,
			IndexPrice:  m.MarketStats.IndexPrice,
			Volume24h:   m.MarketStats.DailyVolume,
			OI:          m.MarketStats.OpenInterest,
			ObservedAt:  now,
		}

		markets = append(markets, market)
	}

	return markets, nil
}

// ParseFundingRate parses Extended funding rate string to float64
func ParseExtendedFundingRate(rate string) (float64, error) {
	return strconv.ParseFloat(rate, 64)
}
