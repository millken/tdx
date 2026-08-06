package tdx

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

// TickChart represents one sampling point on the intraday chart.
type TickChart struct {
	Time   string  // HH:MM
	Price  float64 // yuan (price / (100*scale))
	Avg    float64 // yuan (avg / (10000*scale))
	Volume int64   // lots (手)
}

// RequestTickChartFrame builds a 0x0537 tick chart request frame.
func RequestTickChartFrame(msgID uint32, control byte, market uint16, code string, start uint16, count uint16) ([]byte, error) {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint16(body[0:2], market)
	copy(body[2:8], code)
	binary.LittleEndian.PutUint16(body[8:10], start)
	binary.LittleEndian.PutUint16(body[10:12], count)
	return BuildDirectFrame(msgID, control, DirectFrameTypeTickChart, body), nil
}

// GetTickChart retrieves intraday tick chart data for the given stock code.
//
// Parameters:
//   - code: stock code, supports "600000", "sh600000", "SH600000" formats
//   - count: number of data points to retrieve
//
// Example:
//
//	chart, err := c.GetTickChart("sh600000", 240)
func (c *MainClient) GetTickChart(code string, count int) ([]TickChart, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
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

	packet, err := RequestTickChartFrame(0x01000802, 0x00, market, normalizedCode, 0, uint16(count))
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeTickChart, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty tick chart response")
	}

	if os.Getenv("TDX_DUMP") != "" {
		_ = os.WriteFile("/tmp/tdx_tickchart_body.hex", []byte(hex.EncodeToString(response.Body.Decoded)), 0644)
	}

	return DecodeTickChart(response.Body.Decoded, normalizedCode)
}

// DecodeTickChart decodes a 0x0537 tick chart response body.
//
// Body layout: [count:u16, _:u16] followed by `count` varint records.
// Each record contains 3 varint fields:
//   - price (signed): first record is the base price; subsequent records are deltas
//   - avg (signed): first record is the base avg; subsequent records are deltas
//   - volume (signed, unit: lots 手)
//
// Final units: price / (100*scale) = yuan, avg / (10000*scale) = yuan, where
// scale comes from transactionPriceScale(code) — stocks use scale=1, ETFs scale=10.
func DecodeTickChart(body []byte, code string) ([]TickChart, error) {
	if len(body) < 4 {
		return nil, fmt.Errorf("tick chart body too short: %d", len(body))
	}
	count := int(binary.LittleEndian.Uint16(body[:2]))
	body = body[4:]
	scale := transactionPriceScale(code)
	priceDiv := 100 * scale
	avgDiv := 10000 * scale
	points := make([]TickChart, 0, count)

	var startPrice int64
	var startAvg int64
	for i := 0; i < count; i++ {
		var price int64
		var avg int64
		var volume int64
		var err error

		body, price, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick chart price record %d: %w", i, err)
		}
		body, avg, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick chart avg record %d: %w", i, err)
		}
		body, volume, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick chart volume record %d: %w", i, err)
		}

		points = append(points, TickChart{
			Time:   tickChartLabel(i),
			Price:  float64(startPrice+price) / float64(priceDiv),
			Avg:    float64(startAvg+avg) / float64(avgDiv),
			Volume: volume,
		})

		if startPrice == 0 {
			startPrice = price
		}
		if startAvg == 0 {
			startAvg = avg
		}
	}
	return points, nil
}

func tickChartLabel(index int) string {
	baseMorning := 9*60 + 30
	baseAfternoon := 13 * 60
	minuteOfDay := baseMorning + index
	if index >= 121 {
		minuteOfDay = baseAfternoon + (index - 121)
	}
	hour := minuteOfDay / 60
	minute := minuteOfDay % 60
	return fmt.Sprintf("%02d:%02d", hour, minute)
}
