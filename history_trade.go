package tdx

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// HistoryTrade represents a single historical intraday trade record.
type HistoryTrade struct {
	Time   time.Time
	Price  float64
	Volume int64
	Status int64
}

// RequestHistoryTradeFrame builds a 0x0FB5 history trade request frame.
func RequestHistoryTradeFrame(msgID uint32, control byte, date string, market uint16, code string, start uint16, count uint16) ([]byte, error) {
	tradeDate, err := parseTradeDateUint32(date)
	if err != nil {
		return nil, err
	}
	body := make([]byte, 16)
	binary.LittleEndian.PutUint32(body[0:4], tradeDate)
	body[4] = byte(market)
	copy(body[6:12], code)
	binary.LittleEndian.PutUint16(body[12:14], start)
	binary.LittleEndian.PutUint16(body[14:16], count)
	return BuildDirectFrame(msgID, control, DirectFrameTypeHistoryTrade, body), nil
}

// GetHistoryTrade retrieves historical intraday trade records for the given stock code.
// It is shorthand for GetHistoryTradeFrom with a zero start offset.
//
// Parameters:
//   - date: trade date, format "20060102" or "2006-01-02"
//   - code: stock code, supports "600000", "sh600000", "SH600000" formats
//   - count: number of records to retrieve, max 2000
//
// Example:
//
//	trades, err := c.GetHistoryTrade("20260421", "sh600000", 500)
func (c *MainClient) GetHistoryTrade(date string, code string, count int) ([]HistoryTrade, error) {
	return c.GetHistoryTradeFrom(date, code, 0, count)
}

// GetHistoryTradeFrom retrieves historical intraday trade records offset from
// the start of the day, so callers can page through days with more trades than
// one request can carry. start counts records forward from the first trade of
// the day; page start upwards until a short batch comes back.
func (c *MainClient) GetHistoryTradeFrom(date string, code string, start, count int) ([]HistoryTrade, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	if count <= 0 || count > 2000 {
		return nil, fmt.Errorf("tdx: history trade count must be 1..2000, got %d", count)
	}
	if start < 0 || start > 65535 {
		return nil, fmt.Errorf("tdx: history trade start must be 0..65535, got %d", start)
	}

	normalizedCode, market, hasPrefixedMarket, err := normalizeKlineCode(code)
	if err != nil {
		return nil, err
	}
	if !hasPrefixedMarket {
		market, err = inferKlineMarket(normalizedCode)
		if err != nil {
			return nil, err
		}
	}

	packet, err := RequestHistoryTradeFrame(0x00000000, 0x01, date, market, normalizedCode, uint16(start), uint16(count))
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeHistoryTrade, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty history trade response")
	}

	return DecodeHistoryTrades(response.Body.Decoded, normalizedCode, date)
}

// DecodeHistoryTrades decodes a 0x0FB5 history trade response body.
func DecodeHistoryTrades(body []byte, code string, date string) ([]HistoryTrade, error) {
	if len(body) < 6 {
		return nil, fmt.Errorf("history trade body too short: %d", len(body))
	}
	tradeDate, err := parseDate(date)
	if err != nil {
		return nil, err
	}
	scale := transactionPriceScale(code)

	count := int(binary.LittleEndian.Uint16(body[:2]))
	body = body[6:]
	items := make([]HistoryTrade, 0, count)
	lastPriceMilli := int64(0)

	for i := 0; i < count; i++ {
		if len(body) < 2 {
			return nil, fmt.Errorf("history trade record %d missing time bytes", i)
		}
		minuteOfDay := binary.LittleEndian.Uint16(body[:2])
		body = body[2:]

		var priceDelta int64
		body, priceDelta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("decode history trade price record %d: %w", i, err)
		}
		lastPriceMilli += priceDelta * 10

		var volume, status int64
		body, volume, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode history trade volume record %d: %w", i, err)
		}
		body, status, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode history trade status record %d: %w", i, err)
		}
		body, _, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode history trade reserved record %d: %w", i, err)
		}

		hour := int(minuteOfDay / 60)
		minute := int(minuteOfDay % 60)
		t := time.Date(tradeDate.Year(), tradeDate.Month(), tradeDate.Day(), hour, minute, 0, 0, tradeDate.Location())

		items = append(items, HistoryTrade{
			Time:   t,
			Price:  milliToYuan(lastPriceMilli) / float64(scale),
			Volume: volume,
			Status: status,
		})
	}
	return items, nil
}

func parseTradeDateUint32(date string) (uint32, error) {
	normalized := strings.TrimSpace(date)
	if len(normalized) == 10 && normalized[4] == '-' {
		normalized = normalized[0:4] + normalized[5:7] + normalized[8:10]
	}
	if len(normalized) != 8 {
		return 0, fmt.Errorf("date must be yyyyMMdd: %q", date)
	}
	value := uint32(0)
	for _, ch := range normalized {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("date must be yyyyMMdd: %q", date)
		}
		value = value*10 + uint32(ch-'0')
	}
	return value, nil
}
