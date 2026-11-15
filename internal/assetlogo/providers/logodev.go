package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/assetlogo"
)

var ErrProviderDisabled = fmt.Errorf("provider disabled")

type LogoDevProvider struct {
	SecretKey  string
	Secret     string
	APIKey     string
	BaseURL    string
	Client     *http.Client
	HTTPClient *http.Client
}

func (p *LogoDevProvider) Name() string { return "logodev" }

func (p *LogoDevProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return http.DefaultClient
}

func (p *LogoDevProvider) secret() string {
	if p.SecretKey != "" {
		return p.SecretKey
	}
	if p.Secret != "" {
		return p.Secret
	}
	return p.APIKey
}

func (p *LogoDevProvider) baseURL() string {
	if p.BaseURL != "" {
		return strings.TrimRight(p.BaseURL, "/")
	}
	return "https://api.logo.dev"
}

type logoDevRef struct {
	Ticker string `json:"ticker"`
	ISIN   string `json:"isin"`
	Domain string `json:"domain"`
}

type logoDevResponse struct {
	Data struct {
		URL  string `json:"url"`
		ETag string `json:"etag"`
	} `json:"data"`
}

func (p *LogoDevProvider) parseRef(ref assetlogo.AssetProviderRef) (logoDevRef, error) {
	var r logoDevRef
	if err := json.Unmarshal(ref.Ref, &r); err != nil {
		return r, err
	}
	if r.Ticker == "" && r.ISIN == "" && r.Domain == "" {
		return r, fmt.Errorf("logodev: missing ticker/isin/domain")
	}
	return r, nil
}

func (p *LogoDevProvider) BuildAPIURL(ref assetlogo.AssetProviderRef) (string, error) {
	r, err := p.parseRef(ref)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	if r.Ticker != "" {
		q.Set("ticker", r.Ticker)
	} else if r.ISIN != "" {
		q.Set("isin", r.ISIN)
	} else {
		q.Set("domain", r.Domain)
	}
	return p.baseURL() + "/v2/brands/logo?" + q.Encode(), nil
}

func (p *LogoDevProvider) Resolve(ctx context.Context, asset assetlogo.Asset, ref assetlogo.AssetProviderRef) (assetlogo.ResolvedLogo, error) {
	if p.secret() == "" {
		return assetlogo.ResolvedLogo{}, ErrProviderDisabled
	}
	apiURL, err := p.BuildAPIURL(ref)
	if err != nil {
		return assetlogo.ResolvedLogo{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return assetlogo.ResolvedLogo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.secret())
	resp, err := p.client().Do(req)
	if err != nil {
		return assetlogo.ResolvedLogo{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed logoDevResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return assetlogo.ResolvedLogo{Provider: p.Name(), StatusCode: resp.StatusCode}, err
		}
		downloadURL := parsed.Data.URL
		if downloadURL == "" {
			return assetlogo.ResolvedLogo{Provider: p.Name(), StatusCode: resp.StatusCode}, fmt.Errorf("logodev: empty url")
		}
		// append size/format
		u, err := url.Parse(downloadURL)
		if err == nil {
			q := u.Query()
			q.Set("size", "256")
			q.Set("format", "webp")
			u.RawQuery = q.Encode()
			downloadURL = u.String()
		}
		dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
		if err != nil {
			return assetlogo.ResolvedLogo{}, err
		}
		dlResp, err := p.client().Do(dlReq)
		if err != nil {
			return assetlogo.ResolvedLogo{}, err
		}
		defer dlResp.Body.Close()
		dlBytes, err := io.ReadAll(dlResp.Body)
		if err != nil {
			return assetlogo.ResolvedLogo{}, err
		}
		if dlResp.StatusCode != http.StatusOK {
			return assetlogo.ResolvedLogo{Provider: p.Name(), URL: downloadURL, StatusCode: dlResp.StatusCode}, fmt.Errorf("logodev: download status %d", dlResp.StatusCode)
		}
		return assetlogo.ResolvedLogo{
			Provider:    p.Name(),
			URL:         downloadURL,
			ETag:        parsed.Data.ETag,
			ContentType: dlResp.Header.Get("Content-Type"),
			Bytes:       dlBytes,
			StatusCode:  http.StatusOK,
		}, nil
	case http.StatusAccepted:
		ra := resp.Header.Get("Retry-After")
		dur := time.Duration(0)
		if n, err := strconv.Atoi(ra); err == nil {
			dur = time.Duration(n) * time.Second
		} else if t, err := http.ParseTime(ra); err == nil {
			dur = time.Until(t)
		}
		return assetlogo.ResolvedLogo{Provider: p.Name(), StatusCode: resp.StatusCode, RetryAfter: dur}, fmt.Errorf("logodev: retry after")
	case http.StatusNotFound:
		return assetlogo.ResolvedLogo{Provider: p.Name(), StatusCode: resp.StatusCode}, fmt.Errorf("logodev: not found")
	case http.StatusPaymentRequired:
		return assetlogo.ResolvedLogo{Provider: p.Name(), StatusCode: resp.StatusCode}, fmt.Errorf("logodev: payment required")
	default:
		return assetlogo.ResolvedLogo{Provider: p.Name(), StatusCode: resp.StatusCode}, fmt.Errorf("logodev: unexpected status %d", resp.StatusCode)
	}
}
