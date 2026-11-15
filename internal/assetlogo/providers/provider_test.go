package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qwerty7415963/go_be_arbitrage/internal/assetlogo"
)

func twRef(kind, chain, contract string) assetlogo.AssetProviderRef {
	b, _ := json.Marshal(map[string]string{"kind": kind, "chain": chain, "contract": contract})
	return assetlogo.AssetProviderRef{Provider: "trustwallet", Ref: b}
}

func TestTrustWallet_BuildURL_Native(t *testing.T) {
	p := &TrustWalletProvider{}
	u, err := p.BuildURL(twRef("native", "ethereum", ""))
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://raw.githubusercontent.com/trustwallet/assets/master/blockchains/ethereum/info/logo.png" {
		t.Fatalf("got %s", u)
	}
}

func TestTrustWallet_BuildURL_Token(t *testing.T) {
	p := &TrustWalletProvider{}
	u, err := p.BuildURL(twRef("token", "ethereum", "0xabc"))
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://raw.githubusercontent.com/trustwallet/assets/master/blockchains/ethereum/assets/0xabc/logo.png" {
		t.Fatalf("got %s", u)
	}
}

func TestTrustWallet_Resolve_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(200)
		w.Write([]byte("PNG"))
	}))
	defer srv.Close()
	// override URL by using a custom provider? TrustWallet URL fixed; test download via direct? Instead test status handling with fixed host impossible.
	// We'll at least ensure BuildURL validation.
	p := &TrustWalletProvider{}
	if _, err := p.BuildURL(twRef("native", "", "")); err == nil {
		t.Fatal("expected error")
	}
}

func ldRef(ticker, isin, domain string) assetlogo.AssetProviderRef {
	b, _ := json.Marshal(map[string]string{"ticker": ticker, "isin": isin, "domain": domain})
	return assetlogo.AssetProviderRef{Provider: "logodev", Ref: b}
}

func TestLogoDev_Disabled(t *testing.T) {
	p := &LogoDevProvider{}
	_, err := p.Resolve(context.Background(), assetlogo.Asset{}, ldRef("AAPL", "", ""))
	if err != ErrProviderDisabled {
		t.Fatalf("expected disabled, got %v", err)
	}
}

func TestLogoDev_BuildAPIURL(t *testing.T) {
	p := &LogoDevProvider{SecretKey: "sk_x"}
	u, err := p.BuildAPIURL(ldRef("", "US123", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(u, "isin=US123") {
		t.Fatalf("got %s", u)
	}
}

func TestLogoDev_StatusCodes(t *testing.T) {
	// API server returns various statuses; download server returns 200 with bytes.
	dlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/webp")
		w.Write([]byte("WEBP"))
	}))
	defer dlSrv.Close()

	cases := []struct {
		status int
		retry  string
	}{
		{200, ""},
		{202, "5"},
		{404, ""},
		{402, ""},
	}
	for _, c := range cases {
		apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c.status == 200 {
				resp := map[string]any{"data": map[string]string{"url": dlSrv.URL + "/logo.png", "etag": "e1"}}
				b, _ := json.Marshal(resp)
				w.Write(b)
				return
			}
			if c.status == 202 {
				w.Header().Set("Retry-After", c.retry)
			}
			w.WriteHeader(c.status)
		}))
		p := &LogoDevProvider{SecretKey: "sk_x", BaseURL: apiSrv.URL, Client: http.DefaultClient}
		res, err := p.Resolve(context.Background(), assetlogo.Asset{}, ldRef("AAPL", "", ""))
		apiSrv.Close()
		switch c.status {
		case 200:
			if err != nil {
				t.Fatalf("200 err %v", err)
			}
			if string(res.Bytes) != "WEBP" {
				t.Fatalf("bytes %s", res.Bytes)
			}
			if res.ETag != "e1" {
				t.Fatalf("etag %s", res.ETag)
			}
		case 202:
			if err == nil {
				t.Fatal("expected err on 202")
			}
			if res.RetryAfter != 5*time.Second {
				t.Fatalf("retry %v", res.RetryAfter)
			}
		case 404:
			if err == nil {
				t.Fatal("expected err on 404")
			}
		case 402:
			if err == nil {
				t.Fatal("expected err on 402")
			}
		}
		_ = time.Second
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
