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
	variationalBaseURL         = "https://omni-client-api.prod.ap-northeast-1.variational.io"
	variationalStatsURL        = variationalBaseURL + "/metadata/stats"
	variationalFundingInterval = 28800 // 8 hours in seconds
)

// VariationalAdapter implements REST-based data fetching for Variational
type VariationalAdapter struct {
	code   string
	client *http.Client
}

type VariationalStatsResponse struct {
	Listings []VariationalListing `json:"listings"`
}

type VariationalListing struct {
	Ticker           string                  `json:"ticker"`
	Name             string                  `json:"name"`
	MarkPrice        string                  `json:"mark_price"`
	Volume24h        string                  `json:"volume_24h"`
	FundingRate      string                  `json:"funding_rate"`
	FundingIntervalS int                     `json:"funding_interval_s"`
	OpenInterest     VariationalOpenInterest `json:"open_interest"`
	BaseSpreadBps    string                  `json:"base_spread_bps"`
}

type VariationalOpenInterest struct {
	LongOpenInterest  string `json:"long_open_interest"`
	ShortOpenInterest string `json:"short_open_interest"`
}

type VariationalFundingData struct {
	Symbol      string
	BaseAsset   string
	FundingRate string
	MarkPrice   string
	Volume24h   string
	OI          string
	IntervalS   int
	ObservedAt  time.Time
}

func NewVariationalAdapter() *VariationalAdapter {
	return &VariationalAdapter{
		code:   "variational",
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *VariationalAdapter) GetVenueCode() string {
	return a.code
}

func (a *VariationalAdapter) GetFundingInterval() int {
	return variationalFundingInterval
}

// FetchAllListings fetches all listings with funding data
func (a *VariationalAdapter) FetchAllListings(ctx context.Context) ([]VariationalFundingData, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", variationalStatsURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result VariationalStatsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var listings []VariationalFundingData
	now := time.Now()

	for _, l := range result.Listings {
		// Calculate total OI (long + short)
		longOI, _ := strconv.ParseFloat(l.OpenInterest.LongOpenInterest, 64)
		shortOI, _ := strconv.ParseFloat(l.OpenInterest.ShortOpenInterest, 64)
		totalOI := longOI + shortOI

		listing := VariationalFundingData{
			Symbol:      l.Ticker,
			BaseAsset:   l.Ticker,
			FundingRate: l.FundingRate,
			MarkPrice:   l.MarkPrice,
			Volume24h:   l.Volume24h,
			OI:          strconv.FormatFloat(totalOI, 'f', -1, 64),
			IntervalS:   l.FundingIntervalS,
			ObservedAt:  now,
		}

		listings = append(listings, listing)
	}

	return listings, nil
}

// ParseFundingRate parses Variational funding rate string to float64
func ParseVariationalFundingRate(rate string) (float64, error) {
	return strconv.ParseFloat(rate, 64)
}
