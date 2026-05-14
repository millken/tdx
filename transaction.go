package tdx

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// Transaction represents a single intraday trade record.
type Transaction struct {
	Time   time.Time
	Price  float64
	Volume int64
	Number int64
	Status int64
}

// RequestTransactionFrame builds a 0x0FC5 transaction request frame.
func RequestTransactionFrame(msgID uint32, control byte, market uint16, code string, start uint16, count uint16) ([]byte, error) {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint16(body[0:2], market)
	copy(body[2:8], code)
	binary.LittleEndian.PutUint16(body[8:10], start)
	binary.LittleEndian.PutUint16(body[10:12], count)
	return BuildDirectFrame(msgID, control, DirectFrameTypeTransaction, body), nil
}

// GetTransaction retrieves intraday transaction records for the given stock code.
//
// Parameters:
//   - code: stock code, supports "600000", "sh600000", "SH600000" formats
//   - date: trade date, format "20060102" or "2006-01-02", defaults to today if empty
//   - count: number of records to retrieve, max 900
//
// Example:
//
//	trades, err := c.GetTransaction("sh600000", "", 100)
func (c *MainClient) GetTransaction(code string, date string, count int) ([]Transaction, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	if count <= 0 || count > 900 {
		return nil, fmt.Errorf("tdx: transaction count must be 1..900, got %d", count)
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

	packet, err := RequestTransactionFrame(0x03000802, 0x01, market, normalizedCode, 0, uint16(count))
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeTransaction, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty transaction response")
	}

	return DecodeTransactions(response.Body.Decoded, normalizedCode, date)
}

// DecodeTransactions decodes a 0x0FC5 transaction response body.
func DecodeTransactions(body []byte, code string, date string) ([]Transaction, error) {
	if len(body) < 2 {
		return nil, fmt.Errorf("transaction body too short: %d", len(body))
	}
	tradeDate, err := parseDate(date)
	if err != nil {
		return nil, err
	}
	scale := transactionPriceScale(code)

	count := int(binary.LittleEndian.Uint16(body[:2]))
	body = body[2:]
	items := make([]Transaction, 0, count)
	lastPriceMilli := int64(0)

	for i := 0; i < count; i++ {
		if len(body) < 2 {
			return nil, fmt.Errorf("transaction record %d missing time bytes", i)
		}
		minuteOfDay := binary.LittleEndian.Uint16(body[:2])
		body = body[2:]

		var priceDelta int64
		body, priceDelta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("decode transaction price record %d: %w", i, err)
		}
		lastPriceMilli += priceDelta * 10

		var volume, number, status int64
		body, volume, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode transaction volume record %d: %w", i, err)
		}
		body, number, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode transaction number record %d: %w", i, err)
		}
		body, status, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode transaction status record %d: %w", i, err)
		}
		body, _, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode transaction reserved record %d: %w", i, err)
		}

		hour := int(minuteOfDay / 60)
		minute := int(minuteOfDay % 60)
		t := time.Date(tradeDate.Year(), tradeDate.Month(), tradeDate.Day(), hour, minute, 0, 0, tradeDate.Location())

		items = append(items, Transaction{
			Time:   t,
			Price:  float64(lastPriceMilli) / 1000 / float64(scale),
			Volume: volume,
			Number: number,
			Status: status,
		})
	}
	return items, nil
}

func parseDate(date string) (time.Time, error) {
	if strings.TrimSpace(date) == "" {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil
	}
	for _, layout := range []string{"20060102", "2006-01-02"} {
		parsed, err := time.ParseInLocation(layout, date, time.Local)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q: expected yyyyMMdd or yyyy-MM-dd", date)
}
