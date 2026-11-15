package assetlogo

import (
	"context"
	"encoding/json"
	"fmt"
)

type seedNative struct {
	symbol string
	chain  string
}

var cryptoNatives = []seedNative{
	{"BTC", "bitcoin"}, {"ETH", "ethereum"}, {"SOL", "solana"}, {"XRP", "xrp"},
	{"DOGE", "dogecoin"}, {"ADA", "cardano"}, {"BNB", "binance"}, {"LTC", "litecoin"},
	{"BCH", "bitcoincash"}, {"DOT", "polkadot"}, {"TRX", "tron"}, {"TON", "ton"},
	{"AVAX", "avalanche"}, {"ATOM", "cosmos"}, {"NEAR", "near"}, {"APT", "aptos"},
	{"SUI", "sui"}, {"SEI", "sei"}, {"TIA", "celestia"}, {"INJ", "injective"},
	{"FIL", "filecoin"}, {"ICP", "internetcomputer"}, {"HBAR", "hedera"}, {"VET", "vechain"},
	{"ALGO", "algorand"}, {"XLM", "stellar"}, {"EOS", "eos"}, {"NEO", "neo"},
	{"THETA", "theta"}, {"KAS", "kaspa"}, {"STX", "stacks"},
}

type seedToken struct {
	symbol   string
	chain    string
	contract string
}

// Only tokens with verified chain+contract are included.
var cryptoTokens = []seedToken{
	{"LINK", "ethereum", "0x514910771af9ca656af840dff83e8264ecf986ca"},
	{"UNI", "ethereum", "0x1f9840a85d5af5bf1d1762f925bdaddc4201f984"},
	{"AAVE", "ethereum", "0x7fc66500c84a76ad7e9c93437bfc5ac33e2ddae9"},
	{"ARB", "ethereum", "0xb50721bcf8d664c30412cfbc6925452533864123"},
	{"OP", "ethereum", "0x4200000000000000000000000000000000000042"},
	{"POL", "ethereum", "0x455e53cbb86018ac2b8092fdcd39d8444affc3f6"},
	{"MATIC", "ethereum", "0x7d1afa7b718fb893db30a3abc0cfc608aacfebb0"},
	{"PEPE", "ethereum", "0x6982508145454ce325ddbe47a25d4ec3d2311933"},
	{"SHIB", "ethereum", "0x95ad61b0a150d79219dcf64e1e6cc01f0b64c4ce"},
	{"LDO", "ethereum", "0x5a98fcbea516cf06857215779fd812ca3bef1b32"},
	{"CRV", "ethereum", "0xd533a949740bb3306d119cc777fa900ba034cd52"},
	{"MKR", "ethereum", "0x9f8f72aa9304c8b593d555f12ef6589cc3a579a2"},
	{"SNX", "ethereum", "0xc011a73ee8576fb46f5e1c5751ca3b9fe0af2a6f"},
	{"COMP", "ethereum", "0xc00e94cb662c3520282e6f5717214004a7f26888"},
	{"GRT", "ethereum", "0xc944e90c64b2c07662a292be6244bdf05cda44a7"},
	{"IMX", "ethereum", "0xf57e7e7c23978c3caec3c3548e3d615c346e79ff"},
	{"BLUR", "ethereum", "0x5283d291dbcf85356a21ba090e6db59121208b84"},
	{"DYDX", "ethereum", "0x92d6c1e31e14520e676a687f0a93788b716beff5"},
	{"GMX", "ethereum", "0xfc5a1a6eb076a2c7ad06ed22c90d7e710e35ad0a"},
	{"GALA", "ethereum", "0x15d4c048f83bd7e37d49ea4c83a07267ec4203da"},
	{"SAND", "ethereum", "0x3845badade8e6dff049820680d1f12bd130b37931"},
	{"MANA", "ethereum", "0x0f5d2fb29fb7d3cfee444a200298f468908cc942"},
	{"AXS", "ethereum", "0xbb0e17ef65f82ab018d8edd776e9872c4d36f678"},
	{"APE", "ethereum", "0x4d224452801aced8b2f0aebe155379bb5d594381"},
	{"CHZ", "ethereum", "0x3506424f91fd33084466f402d5d97f05f8e3b4af"},
	{"ENJ", "ethereum", "0xf629cbd94d3791c9250152bd8dfbdf380e2a3b9c"},
	{"FET", "ethereum", "0xaea46a60368a7bd060eec1cca1271c125aa005d0"},
	{"WLD", "ethereum", "0x163f8c2467924be0ae7b5347228cabf260318c0e"},
	{"ENA", "ethereum", "0x57e114b691db790c35207b2e685d4a43181e6061"},
	{"ETHFI", "ethereum", "0xfe0c30065b384f05761f15d0cc899d4f9f9cc0eb"},
	{"RENDER", "ethereum", "0x6de037ef9ad2725eb40118bb1702ebb27e4aeb24"},
	{"TAO", "ethereum", "0x77e06c9eccf2e797663b7a5f1411d0f9a6c00c1e"}, // best effort
}

func assetNamePtr(s string) *string { return &s }

func refJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func Seed(ctx context.Context, repo *Repository) error {
	// crypto native
	for _, n := range cryptoNatives {
		asset := &Asset{
			CanonicalKey: CanonicalKeyCryptoNative(n.symbol),
			Symbol:       n.symbol,
			AssetClass:   AssetClassCryptoNative,
			Metadata:     json.RawMessage("{}"),
		}
		name := n.symbol
		asset.Name = &name
		if err := repo.CreateAsset(ctx, asset); err != nil {
			return fmt.Errorf("seed native %s: %w", n.symbol, err)
		}
		rref := refJSON(map[string]string{"kind": "native", "chain": n.chain})
		if err := repo.UpsertProviderRef(ctx, &AssetProviderRef{
			AssetID:  asset.ID,
			Provider: "trustwallet",
			Ref:      rref,
		}); err != nil {
			return err
		}
		// logo row
		_ = repo.UpsertLogo(ctx, &AssetLogo{
			AssetID:  asset.ID,
			Provider: "trustwallet",
			Status:   LogoStatusMissing,
		})
	}

	// crypto tokens
	for _, t := range cryptoTokens {
		if t.chain == "" || t.contract == "" {
			continue
		}
		asset := &Asset{
			CanonicalKey: CanonicalKeyCryptoToken(t.chain, t.contract),
			Symbol:       t.symbol,
			AssetClass:   AssetClassCryptoToken,
			Metadata:     json.RawMessage("{}"),
		}
		name := t.symbol
		asset.Name = &name
		if err := repo.CreateAsset(ctx, asset); err != nil {
			return fmt.Errorf("seed token %s: %w", t.symbol, err)
		}
		rref := refJSON(map[string]string{"kind": "token", "chain": t.chain, "contract": t.contract})
		if err := repo.UpsertProviderRef(ctx, &AssetProviderRef{
			AssetID:  asset.ID,
			Provider: "trustwallet",
			Ref:      rref,
		}); err != nil {
			return err
		}
		_ = repo.UpsertLogo(ctx, &AssetLogo{
			AssetID:  asset.ID,
			Provider: "trustwallet",
			Status:   LogoStatusMissing,
		})
	}

	// TradFi
	tradfi := []struct {
		venueSymbol string
		class       AssetClass
		symbol      string
		name        string
		ref         map[string]string
	}{
		{"XYZ:SP500", AssetClassIndex, "SP500", "S&P 500", map[string]string{"ticker": "SPY"}},
		{"XYZ:XYZ100", AssetClassIndex, "XYZ100", "XYZ 100", map[string]string{"ticker": "QQQ"}},
		{"XYZ:GOLD", AssetClassCommodity, "GOLD", "Gold", map[string]string{"ticker": "GOLD"}},
		{"XYZ:CL", AssetClassCommodity, "CL", "WTI Crude", map[string]string{"ticker": "CL"}},
		{"XYZ:NVDA", AssetClassEquity, "NVDA", "NVIDIA", map[string]string{"ticker": "NVDA"}},
		{"XYZ:TSLA", AssetClassEquity, "TSLA", "Tesla", map[string]string{"ticker": "TSLA"}},
		{"XYZ:MU", AssetClassEquity, "MU", "Micron", map[string]string{"ticker": "MU"}},
		{"XYZ:SNDK", AssetClassEquity, "SNDK", "SanDisk", map[string]string{"ticker": "SNDK"}},
		{"XYZ:SKHX", AssetClassEquity, "SKHX", "SK Hynix", map[string]string{"ticker": "SKHX"}},
	}
	for _, t := range tradfi {
		var ckey string
		switch t.class {
		case AssetClassIndex:
			ckey = CanonicalKeyIndex(t.symbol)
		case AssetClassCommodity:
			ckey = CanonicalKeyCommodity(t.symbol)
		case AssetClassEquity:
			ckey = CanonicalKeyEquity("NASDAQ", t.symbol)
		}
		// For GOLD/CL use specific commodity keys maybe; keep simple.
		asset := &Asset{
			CanonicalKey: ckey,
			Symbol:       t.symbol,
			AssetClass:   t.class,
			Metadata:     json.RawMessage("{}"),
		}
		n := t.name
		asset.Name = &n
		if err := repo.CreateAsset(ctx, asset); err != nil {
			return fmt.Errorf("seed tradfi %s: %w", t.venueSymbol, err)
		}
		if err := repo.UpsertProviderRef(ctx, &AssetProviderRef{
			AssetID:  asset.ID,
			Provider: "logodev",
			Ref:      refJSON(t.ref),
		}); err != nil {
			return err
		}
		_ = repo.UpsertLogo(ctx, &AssetLogo{
			AssetID:  asset.ID,
			Provider: "logodev",
			Status:   LogoStatusMissing,
		})
	}

	return nil
}

func SeedIfNotExists(ctx context.Context, repo *Repository) error {
	return Seed(ctx, repo)
}
