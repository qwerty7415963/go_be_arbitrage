package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	binanceBaseURL         = "https://fapi.binance.com"
	binanceFundingURL      = binanceBaseURL + "/fapi/v1/premiumIndex"
	binanceExchangeInfo    = binanceBaseURL + "/fapi/v1/exchangeInfo"
	binanceTickerURL       = binanceBaseURL + "/fapi/v1/ticker/24hr"
	binanceOpenInterest    = binanceBaseURL + "/fapi/v1/openInterest"
	binanceFundingInterval = 28800 // 8 hours in seconds
)

// BinanceAdapter implements REST-based data fetching for Binance futures
type BinanceAdapter struct {
	code     string
	client   *http.Client
	mu       sync.RWMutex
	perpList []BinanceSymbol
}

type BinanceSymbol struct {
	Symbol       string `json:"symbol"`
	BaseAsset    string `json:"baseAsset"`
	QuoteAsset   string `json:"quoteAsset"`
	ContractType string `json:"contractType"`
	Pair         string `json:"pair"`
}

type BinancePremiumIndex struct {
	Symbol               string `json:"symbol"`
	MarkPrice            string `json:"markPrice"`
	IndexPrice           string `json:"indexPrice"`
	EstimatedSettlePrice string `json:"estimatedSettlePrice"`
	LastFundingRate      string `json:"lastFundingRate"`
	InterestRate         string `json:"interestRate"`
	NextFundingTime      int64  `json:"nextFundingTime"`
	Time                 int64  `json:"time"`
}

type BinanceTicker24h struct {
	Symbol             string `json:"symbol"`
	LastPrice          string `json:"lastPrice"`
	PriceChange        string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	QuoteVolume        string `json:"quoteVolume"`
	Volume             string `json:"volume"`
	WeightedAvgPrice   string `json:"weightedAvgPrice"`
	HighPrice          string `json:"highPrice"`
	LowPrice           string `json:"lowPrice"`
	OpenPrice          string `json:"openPrice"`
	CloseTime          int64  `json:"closeTime"`
}

type BinanceOpenInterest struct {
	Symbol       string `json:"symbol"`
	OpenInterest string `json:"openInterest"`
	Time         int64  `json:"time"`
}

type BinanceFundingData struct {
	Symbol      string
	FundingRate string
	MarkPrice   string
	IndexPrice  string
	NextFunding int64
	ObservedAt  time.Time
}

func NewBinanceAdapter() *BinanceAdapter {
	return &BinanceAdapter{
		code:   "binance",
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *BinanceAdapter) GetVenueCode() string {
	return a.code
}

func (a *BinanceAdapter) GetFundingInterval() int {
	return binanceFundingInterval
}

// FetchPerpSymbols fetches all perpetual symbols from Binance
func (a *BinanceAdapter) FetchPerpSymbols(ctx context.Context) ([]BinanceSymbol, error) {
	a.mu.RLock()
	if len(a.perpList) > 0 {
		defer a.mu.RUnlock()
		return a.perpList, nil
	}
	a.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, "GET", binanceExchangeInfo, nil)
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

	var result struct {
		Symbols []BinanceSymbol `json:"symbols"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var perps []BinanceSymbol
	for _, s := range result.Symbols {
		if s.ContractType == "PERPETUAL" {
			perps = append(perps, s)
		}
	}

	a.mu.Lock()
	a.perpList = perps
	a.mu.Unlock()

	return perps, nil
}

// FetchAllFunding fetches funding rates for all perpetual symbols
func (a *BinanceAdapter) FetchAllFunding(ctx context.Context) ([]BinanceFundingData, error) {
	perps, err := a.FetchPerpSymbols(ctx)
	if err != nil {
		return nil, err
	}

	var results []BinanceFundingData
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Binance premiumIndex returns all symbols in one call
	req, err := http.NewRequestWithContext(ctx, "GET", binanceFundingURL, nil)
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

	var premiums []BinancePremiumIndex
	if err := json.Unmarshal(body, &premiums); err != nil {
		return nil, err
	}

	// Map premiums by symbol
	premiumMap := make(map[string]BinancePremiumIndex)
	for _, p := range premiums {
		premiumMap[p.Symbol] = p
	}

	// Fetch tickers for volume data
	tickerReq, err := http.NewRequestWithContext(ctx, "GET", binanceTickerURL, nil)
	if err != nil {
		return nil, err
	}

	tickerResp, err := a.client.Do(tickerReq)
	if err != nil {
		return nil, err
	}
	defer tickerResp.Body.Close()

	tickerBody, err := io.ReadAll(tickerResp.Body)
	if err != nil {
		return nil, err
	}

	var tickers []BinanceTicker24h
	if err := json.Unmarshal(tickerBody, &tickers); err != nil {
		return nil, err
	}

	tickerMap := make(map[string]BinanceTicker24h)
	for _, t := range tickers {
		tickerMap[t.Symbol] = t
	}

	// Fetch OI for each perp in parallel
	oiChan := make(chan BinanceOpenInterest, len(perps))
	oiSemaphore := make(chan struct{}, 20) // max 20 concurrent

	for _, p := range perps {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			oiSemaphore <- struct{}{}
			defer func() { <-oiSemaphore }()

			oi, err := a.fetchOpenInterest(ctx, symbol)
			if err == nil {
				mu.Lock()
				oiChan <- *oi
				mu.Unlock()
			}
		}(p.Symbol)
	}

	wg.Wait()
	close(oiChan)

	oiMap := make(map[string]BinanceOpenInterest)
	for oi := range oiChan {
		oiMap[oi.Symbol] = oi
	}

	// Combine all data
	now := time.Now()
	for _, p := range perps {
		premium, hasPremium := premiumMap[p.Symbol]
		_ = tickerMap[p.Symbol] // ticker data available if needed
		_ = oiMap[p.Symbol]     // OI data available if needed

		if !hasPremium {
			continue
		}

		data := BinanceFundingData{
			Symbol:      p.Symbol,
			FundingRate: premium.LastFundingRate,
			MarkPrice:   premium.MarkPrice,
			IndexPrice:  premium.IndexPrice,
			NextFunding: premium.NextFundingTime,
			ObservedAt:  now,
		}

		results = append(results, data)
	}

	return results, nil
}

func (a *BinanceAdapter) fetchOpenInterest(ctx context.Context, symbol string) (*BinanceOpenInterest, error) {
	url := fmt.Sprintf("%s?symbol=%s", binanceOpenInterest, symbol)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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

	var oi BinanceOpenInterest
	if err := json.Unmarshal(body, &oi); err != nil {
		return nil, err
	}

	return &oi, nil
}

// FetchTicker fetches 24h ticker for a specific symbol
func (a *BinanceAdapter) FetchTicker(ctx context.Context, symbol string) (*BinanceTicker24h, error) {
	url := fmt.Sprintf("%s?symbol=%s", binanceTickerURL, symbol)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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

	var ticker BinanceTicker24h
	if err := json.Unmarshal(body, &ticker); err != nil {
		return nil, err
	}

	return &ticker, nil
}

// ParseFundingRate parses Binance funding rate string to float64
func ParseBinanceFundingRate(rate string) (float64, error) {
	return strconv.ParseFloat(rate, 64)
}
