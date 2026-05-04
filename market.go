package tdx

import (
	"fmt"
	"strings"
)

// normalizeKlineCode normalizes a stock code and extracts market prefix.
// Returns: (normalized 6-digit code, market uint16, hasPrefixedMarket bool, error)
func normalizeKlineCode(code string) (string, uint16, bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(code))
	switch {
	case len(normalized) == 8 && strings.HasPrefix(normalized, "sh"):
		return normalized[2:], MarketShanghai, true, nil
	case len(normalized) == 8 && strings.HasPrefix(normalized, "sz"):
		return normalized[2:], MarketShenzhen, true, nil
	case len(normalized) == 8 && strings.HasPrefix(normalized, "bj"):
		return normalized[2:], MarketBeijing, true, nil
	case len(normalized) == 6:
		return normalized, 0, false, nil
	default:
		return "", 0, false, fmt.Errorf("invalid code length: %q", code)
	}
}

// inferKlineMarket infers the market from a 6-digit stock code.
func inferKlineMarket(code string) (uint16, error) {
	if len(code) != 6 {
		return 0, fmt.Errorf("invalid code length: %q", code)
	}
	switch {
	case strings.HasPrefix(code, "92") || strings.HasPrefix(code, "899"):
		return MarketBeijing, nil
	case strings.HasPrefix(code, "5") || strings.HasPrefix(code, "6") || strings.HasPrefix(code, "7") || strings.HasPrefix(code, "9"):
		return MarketShanghai, nil
	case strings.HasPrefix(code, "0") || strings.HasPrefix(code, "1") || strings.HasPrefix(code, "2") || strings.HasPrefix(code, "3") || strings.HasPrefix(code, "4") || strings.HasPrefix(code, "8"):
		return MarketShenzhen, nil
	}
	return 0, fmt.Errorf("unsupported market inference for code %q", code)
}

func inferFundCategory(code string, market uint16) byte {
	if len(code) != 6 {
		return 0x21
	}

	if market == 0 {
		inferred, err := inferKlineMarket(code)
		if err == nil {
			market = inferred
		}
	}

	if market == MarketShanghai && strings.HasPrefix(code, "73") {
		return 0x22
	}

	return 0x21
}

// inferKlineKind infers whether a code is "stock" or "index".
func inferKlineKind(code string, market uint16) string {
	if market == 0 {
		inferred, err := inferKlineMarket(code)
		if err == nil {
			market = inferred
		}
	}
	if code == "999999" {
		return "index"
	}
	if strings.HasPrefix(code, "880") || strings.HasPrefix(code, "881") || strings.HasPrefix(code, "882") || strings.HasPrefix(code, "883") {
		return "index"
	}
	switch market {
	case MarketShanghai:
		if strings.HasPrefix(code, "000") {
			return "index"
		}
	case MarketShenzhen:
		if strings.HasPrefix(code, "399") {
			return "index"
		}
	case MarketBeijing:
		if strings.HasPrefix(code, "899") {
			return "index"
		}
	}
	return "stock"
}

// transactionPriceScale returns the price scale factor for a given code.
// ETFs use scale=10, stocks use scale=1.
func transactionPriceScale(code string) int64 {
	if len(code) != 6 {
		return 1
	}
	switch {
	case strings.HasPrefix(code, "15"), strings.HasPrefix(code, "16"):
		return 10
	case strings.HasPrefix(code, "50"), strings.HasPrefix(code, "51"), strings.HasPrefix(code, "52"), strings.HasPrefix(code, "53"), strings.HasPrefix(code, "56"), strings.HasPrefix(code, "58"):
		return 10
	default:
		return 1
	}
}

// marketPrefix returns the exchange prefix string for a market.
func marketPrefix(market uint16) string {
	switch market {
	case MarketShanghai:
		return "sh"
	case MarketShenzhen:
		return "sz"
	case MarketBeijing:
		return "bj"
	default:
		return ""
	}
}

// isLikelyFundKlineCode matches the verified fund code ranges that require
// the 7727/0x2489 fund KLine protocol instead of the 7709 stock KLine route.
func isLikelyFundKlineCode(code string, market uint16) bool {
	if len(code) != 6 {
		return false
	}

	switch market {
	case MarketShenzhen:
		return (code >= "005000" && code <= "099999") ||
			strings.HasPrefix(code, "15") ||
			strings.HasPrefix(code, "16")
	case MarketShanghai:
		return strings.HasPrefix(code, "50") ||
			strings.HasPrefix(code, "51") ||
			strings.HasPrefix(code, "52") ||
			strings.HasPrefix(code, "53") ||
			strings.HasPrefix(code, "73") ||
			strings.HasPrefix(code, "56") ||
			strings.HasPrefix(code, "58")
	default:
		return false
	}
}
