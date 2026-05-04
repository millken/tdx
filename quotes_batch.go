package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// BatchQuote represents a compact quote for a single security (0x054C).
type BatchQuote struct {
	Market        uint16
	Code          string
	Active        uint16
	Price         float64
	PreClose      float64
	Open          float64
	High          float64
	Low           float64
	ServerTime    string
	Volume        int64
	CurVolume     int64
	Amount        float32
	InsideVolume  int64
	OutsideVolume int64
	BidPrice      float64
	BidVol        int64
	AskPrice      float64
	AskVol        int64
	RiseSpeed     float32
	ShortTurnover float32
	Min2Amount    float32
	VolRatio      float32
	Depth         float32
}

// RequestBatchQuoteFrame builds a 0x054C batch quote request frame.
func RequestBatchQuoteFrame(msgID uint32, control byte, stocks []QuoteStock) ([]byte, error) {
	if len(stocks) == 0 {
		return nil, fmt.Errorf("stocks is empty")
	}
	body := make([]byte, 10+len(stocks)*7)
	binary.LittleEndian.PutUint16(body[0:2], 5)
	binary.LittleEndian.PutUint16(body[8:10], uint16(len(stocks)))
	pos := 10
	for _, s := range stocks {
		body[pos] = s.Market
		copy(body[pos+1:pos+7], s.Code)
		pos += 7
	}
	return BuildDirectFrame(msgID, control, DirectFrameTypeBatchQuote, body), nil
}

// GetBatchQuotes retrieves compact quotes for multiple stock codes in one request (0x054C).
func (c *MainClient) GetBatchQuotes(codes []string) ([]BatchQuote, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	stocks := make([]QuoteStock, 0, len(codes))
	for _, code := range codes {
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
		stocks = append(stocks, QuoteStock{Market: byte(market), Code: normalizedCode})
	}

	c.drainPending()

	packet, err := RequestBatchQuoteFrame(0x000A0401, 0x01, stocks)
	if err != nil {
		return nil, err
	}
	if err := c.sendRaw(packet); err != nil {
		return nil, err
	}

	response, err := c.waitForCMD(DirectFrameTypeBatchQuote, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty batch quote response")
	}

	return DecodeBatchQuotes(response.Body.Decoded)
}

// DecodeBatchQuotes decodes a 0x054C batch quote response body.
func DecodeBatchQuotes(body []byte) ([]BatchQuote, error) {
	if len(body) < 4 {
		return nil, fmt.Errorf("batch quote body too short: %d", len(body))
	}
	count := int(binary.LittleEndian.Uint16(body[2:4]))
	body = body[4:]

	quotes := make([]BatchQuote, 0, count)
	for i := 0; i < count; i++ {
		if len(body) < 9 {
			return nil, fmt.Errorf("batch quote record %d too short for header", i)
		}
		q := BatchQuote{
			Market: uint16(body[0]),
			Code:   string(body[1:7]),
			Active: binary.LittleEndian.Uint16(body[7:9]),
		}
		body = body[9:]

		// Price fields use varint encoding (same as 0x054B)
		var priceBase int64
		var err error

		// price (close/current)
		body, priceBase, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d price: %w", i, err)
		}
		priceFloat := float64(priceBase) / 100.0

		// pre_close (delta from price)
		var delta int64
		body, delta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d pre_close: %w", i, err)
		}
		q.PreClose = priceFloat + float64(delta)/100.0

		// open
		body, delta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d open: %w", i, err)
		}
		q.Open = priceFloat + float64(delta)/100.0

		// high
		body, delta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d high: %w", i, err)
		}
		q.High = priceFloat + float64(delta)/100.0

		// low
		body, delta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d low: %w", i, err)
		}
		q.Low = priceFloat + float64(delta)/100.0

		q.Price = priceFloat

		// server_time
		var serverTime int64
		body, serverTime, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d server_time: %w", i, err)
		}
		q.ServerTime = formatQuoteTime(serverTime)

		// neg_price (盘后交易量)
		body, _, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d neg_price: %w", i, err)
		}

		// volume (total)
		body, q.Volume, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d volume: %w", i, err)
		}

		// cur_volume
		body, q.CurVolume, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d cur_volume: %w", i, err)
		}

		// amount (float32)
		if len(body) < 4 {
			return nil, fmt.Errorf("batch quote %d amount: body too short", i)
		}
		q.Amount = math.Float32frombits(binary.LittleEndian.Uint32(body[:4]))
		body = body[4:]

		// inside_volume
		body, q.InsideVolume, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d inside_volume: %w", i, err)
		}

		// outside_volume
		body, q.OutsideVolume, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d outside_volume: %w", i, err)
		}

		// s_amount (reversed_bytes2)
		body, _, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d s_amount: %w", i, err)
		}

		// open_amount (reversed_bytes3)
		body, _, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d open_amount: %w", i, err)
		}

		// 1-level bid/ask
		var bidDelta, askDelta int64
		body, bidDelta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d bid_price: %w", i, err)
		}
		body, askDelta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d ask_price: %w", i, err)
		}
		q.BidPrice = priceFloat + float64(bidDelta)/100.0
		q.AskPrice = priceFloat + float64(askDelta)/100.0

		body, q.BidVol, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d bid_vol: %w", i, err)
		}
		body, q.AskVol, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("batch quote %d ask_vol: %w", i, err)
		}

		// tail: 2 + 2 + 4 + 4 + 10 + 4 + 4 + 24 + 2 = 56 bytes
		if len(body) < 56 {
			return nil, fmt.Errorf("batch quote %d tail: body too short (%d)", i, len(body))
		}

		// unknown: uint16 (2 bytes)
		// rise_speed: int16 (2 bytes)
		q.RiseSpeed = float32(int16(binary.LittleEndian.Uint16(body[2:4]))) / 100.0

		// short_turnover: int16 (2 bytes)
		q.ShortTurnover = float32(int16(binary.LittleEndian.Uint16(body[4:6]))) / 100.0

		// min2_amount: float32 (4 bytes)
		q.Min2Amount = math.Float32frombits(binary.LittleEndian.Uint32(body[6:10]))

		// opening_rush: int16 (2 bytes)
		// padding: 10 bytes
		// vol_rise_speed: float32 (4 bytes)
		q.VolRatio = math.Float32frombits(binary.LittleEndian.Uint32(body[22:26]))

		// depth: float32 (4 bytes)
		q.Depth = math.Float32frombits(binary.LittleEndian.Uint32(body[26:30]))

		// active2: uint16 (2 bytes at offset 54)
		body = body[56:]

		quotes = append(quotes, q)
	}
	return quotes, nil
}

// formatQuoteTime formats the server time field from 0x054C response.
func formatQuoteTime(ts int64) string {
	if ts == 0 || ts == 100 {
		return "00:00:00"
	}
	s := fmt.Sprintf("%d", ts)
	if len(s) < 7 {
		return s
	}
	result := s[:len(s)-6] + ":"
	minPart := s[len(s)-6:]
	m := int(minPart[0]-'0')*10 + int(minPart[1]-'0')
	if m < 60 {
		result += minPart[:2] + ":"
		sec := int(minPart[2]-'0')*100000 + int(minPart[3]-'0')*10000 + int(minPart[4]-'0')*1000 + int(minPart[5]-'0')*100
		result += fmt.Sprintf("%06.3f", float64(sec)*60.0/10000.0)
	} else {
		totalSec := int(minPart[0]-'0')*100000 + int(minPart[1]-'0')*10000 + int(minPart[2]-'0')*1000 + int(minPart[3]-'0')*100 + int(minPart[4]-'0')*10 + int(minPart[5]-'0')
		mm := totalSec * 60 / 1000000
		ss := (totalSec * 60 % 1000000) * 60 / 1000000
		result += fmt.Sprintf("%02d:%02d", mm, ss)
	}
	return result
}

var _ = time.Local
