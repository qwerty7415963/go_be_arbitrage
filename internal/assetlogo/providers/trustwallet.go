package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/qwerty7415963/go_be_arbitrage/internal/assetlogo"
)

type TrustWalletProvider struct {
	Client     *http.Client
	HTTPClient *http.Client
}

func (p *TrustWalletProvider) Name() string { return "trustwallet" }

func (p *TrustWalletProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return http.DefaultClient
}

type trustWalletRef struct {
	Kind     string `json:"kind"`
	Coin     string `json:"coin"`
	Chain    string `json:"chain"`
	Contract string `json:"contract"`
}

func (p *TrustWalletProvider) parseRef(ref assetlogo.AssetProviderRef) (trustWalletRef, error) {
	var r trustWalletRef
	if err := json.Unmarshal(ref.Ref, &r); err != nil {
		return r, err
	}
	if r.Chain == "" {
		return r, fmt.Errorf("trustwallet: missing chain")
	}
	if r.Kind == "token" && r.Contract == "" {
		return r, fmt.Errorf("trustwallet: missing contract")
	}
	return r, nil
}

func (p *TrustWalletProvider) BuildURL(ref assetlogo.AssetProviderRef) (string, error) {
	r, err := p.parseRef(ref)
	if err != nil {
		return "", err
	}
	if r.Kind == "token" {
		return "https://raw.githubusercontent.com/trustwallet/assets/master/blockchains/" + r.Chain + "/assets/" + r.Contract + "/logo.png", nil
	}
	return "https://raw.githubusercontent.com/trustwallet/assets/master/blockchains/" + r.Chain + "/info/logo.png", nil
}

func (p *TrustWalletProvider) Resolve(ctx context.Context, asset assetlogo.Asset, ref assetlogo.AssetProviderRef) (assetlogo.ResolvedLogo, error) {
	url, err := p.BuildURL(ref)
	if err != nil {
		return assetlogo.ResolvedLogo{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return assetlogo.ResolvedLogo{}, err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return assetlogo.ResolvedLogo{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return assetlogo.ResolvedLogo{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return assetlogo.ResolvedLogo{Provider: p.Name(), URL: url, StatusCode: resp.StatusCode}, fmt.Errorf("trustwallet: unexpected status %d", resp.StatusCode)
	}
	return assetlogo.ResolvedLogo{
		Provider:    p.Name(),
		URL:         url,
		ContentType: resp.Header.Get("Content-Type"),
		Bytes:       body,
		StatusCode:  resp.StatusCode,
	}, nil
}
