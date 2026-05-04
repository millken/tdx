package tdx

import "testing"

func TestIsLikelyFundKlineCode(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		market uint16
		want   bool
	}{
		{name: "open fund sz", code: "009708", market: MarketShenzhen, want: true},
		{name: "lof sz", code: "161910", market: MarketShenzhen, want: true},
		{name: "etf sz", code: "159001", market: MarketShenzhen, want: true},
		{name: "etf sh", code: "510300", market: MarketShanghai, want: true},
		{name: "monetary fund sh", code: "730103", market: MarketShanghai, want: true},
		{name: "stock sh", code: "600000", market: MarketShanghai, want: false},
		{name: "stock sz", code: "000001", market: MarketShenzhen, want: false},
	}

	for _, tt := range tests {
		if got := isLikelyFundKlineCode(tt.code, tt.market); got != tt.want {
			t.Fatalf("%s: isLikelyFundKlineCode(%q, %d) = %v, want %v", tt.name, tt.code, tt.market, got, tt.want)
		}
	}
}

func TestInferKlineMarket(t *testing.T) {
	tests := []struct {
		code string
		want uint16
	}{
		{code: "730103", want: MarketShanghai},
		{code: "009708", want: MarketShenzhen},
	}

	for _, tt := range tests {
		got, err := inferKlineMarket(tt.code)
		if err != nil {
			t.Fatalf("inferKlineMarket(%q) error: %v", tt.code, err)
		}
		if got != tt.want {
			t.Fatalf("inferKlineMarket(%q) = %d, want %d", tt.code, got, tt.want)
		}
	}
}

func TestInferFundCategory(t *testing.T) {
	tests := []struct {
		code   string
		market uint16
		want   byte
	}{
		{code: "009708", market: MarketShenzhen, want: 0x21},
		{code: "161910", market: MarketShenzhen, want: 0x21},
		{code: "730103", market: MarketShanghai, want: 0x22},
	}

	for _, tt := range tests {
		if got := inferFundCategory(tt.code, tt.market); got != tt.want {
			t.Fatalf("inferFundCategory(%q, %d) = 0x%02x, want 0x%02x", tt.code, tt.market, got, tt.want)
		}
	}
}
