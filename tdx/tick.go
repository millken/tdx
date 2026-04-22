package tdx

import (
	"encoding/binary"
	"fmt"
	"time"
)

// TickLevel represents one level of the order book (buy or sell).
type TickLevel struct {
	Price  float64
	Volume int64
}

// Tick represents a real-time quote snapshot for a single security.
type Tick struct {
	Market        uint16
	Code          string
	Active1       uint16
	Time          string
	PrevClose     float64
	Open          float64
	High          float64
	Low           float64
	Price         float64
	TotalVolume   int64
	Volume        int64
	Amount        float64
	InsideVolume  int64
	OutsideVolume int64
	BuyLevels     [5]TickLevel
	SellLevels    [5]TickLevel
	// ChangeRate is the raw change rate field from the protocol tail.
	// Use CalcChangeRate() for the actual price change percentage.
	ChangeRate    float64
	Active2       uint16
}

// CalcChangeRate returns the price change percentage relative to PrevClose.
// Returns 0 if PrevClose is zero.
func (t *Tick) CalcChangeRate() float64 {
	if t.PrevClose == 0 {
		return 0
	}
	return (t.Price - t.PrevClose) / t.PrevClose * 100
}

// RequestTickFrame builds a 0x053E tick request frame for one or more stocks.
func RequestTickFrame(msgID uint32, control byte, stocks []QuoteStock) ([]byte, error) {
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
	return BuildDirectFrame(msgID, control, DirectFrameTypeTick, body), nil
}

// QuoteStock is a market+code pair used in batch requests.
type QuoteStock struct {
	Market byte
	Code   string
}

// GetTick retrieves a real-time quote snapshot for the given stock code.
//
// Parameters:
//   - code: stock code, supports "600000", "sh600000", "SH600000" formats
//
// Example:
//
//	tick, err := c.GetTick("sh600000")
func (c *Client) GetTick(code string) (*Tick, error) {
	ticks, err := c.GetTicks([]string{code})
	if err != nil {
		return nil, err
	}
	if len(ticks) == 0 {
		return nil, fmt.Errorf("tdx: empty tick response")
	}
	return &ticks[0], nil
}

// GetTicks retrieves real-time quote snapshots for multiple stock codes in one request.
//
// Parameters:
//   - codes: stock codes, supports "600000", "sh600000", "SH600000" formats
//
// Example:
//
//	ticks, err := c.GetTicks([]string{"sh600000", "sz000001", "sz300750"})
func (c *Client) GetTicks(codes []string) ([]Tick, error) {
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

	packet, err := RequestTickFrame(0x000A0401, 0x01, stocks)
	if err != nil {
		return nil, err
	}
	if err := c.sendRaw(packet); err != nil {
		return nil, err
	}

	response, err := c.waitForCMD(DirectFrameTypeTick, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty tick response")
	}

	return DecodeTicks(response.Body.Decoded)
}

// DecodeTicks decodes a 0x053E tick response body.
func DecodeTicks(body []byte) ([]Tick, error) {
	if len(body) < 4 {
		return nil, fmt.Errorf("tick body too short: %d", len(body))
	}
	body = body[2:]
	count := int(binary.LittleEndian.Uint16(body[:2]))
	body = body[2:]
	ticks := make([]Tick, 0, count)

	for i := 0; i < count; i++ {
		if len(body) < 9 {
			return nil, fmt.Errorf("tick record %d too short for header: %d", i, len(body))
		}
		tick := Tick{
			Market:  uint16(body[0]),
			Code:    string(body[1:7]),
			Active1: binary.LittleEndian.Uint16(body[7:9]),
		}
		body = body[9:]

		var qk tickQuoteK
		var err error
		body, qk, err = decodeTickQuoteK(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d kline header: %w", i, err)
		}
		tick.PrevClose = milliToYuan(qk.prevCloseMilli)
		tick.Open = milliToYuan(qk.openMilli)
		tick.High = milliToYuan(qk.highMilli)
		tick.Low = milliToYuan(qk.lowMilli)
		tick.Price = milliToYuan(qk.priceMilli)

		var v int64
		body, v, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d server time: %w", i, err)
		}
		tick.Time = fmt.Sprintf("%d", v)

		body, _, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d reserved1: %w", i, err)
		}
		body, tick.TotalVolume, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d total volume: %w", i, err)
		}
		body, tick.Volume, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d volume: %w", i, err)
		}

		if len(body) < 4 {
			return nil, fmt.Errorf("decode tick record %d amount: body too short", i)
		}
		tick.Amount = decodeTDXVolume(binary.LittleEndian.Uint32(body[:4]))
		body = body[4:]

		body, tick.InsideVolume, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d inside volume: %w", i, err)
		}
		body, tick.OutsideVolume, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d outside volume: %w", i, err)
		}
		body, _, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d reserved2: %w", i, err)
		}
		body, _, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d reserved3: %w", i, err)
		}

		for li := 0; li < 5; li++ {
			var buyDelta, sellDelta int64
			var buyVol, sellVol int64
			body, buyDelta, err = cutPrice(body)
			if err != nil {
				return nil, fmt.Errorf("decode tick record %d buy level %d price: %w", i, li+1, err)
			}
			body, sellDelta, err = cutPrice(body)
			if err != nil {
				return nil, fmt.Errorf("decode tick record %d sell level %d price: %w", i, li+1, err)
			}
			body, buyVol, err = cutVarintInt(body)
			if err != nil {
				return nil, fmt.Errorf("decode tick record %d buy level %d volume: %w", i, li+1, err)
			}
			body, sellVol, err = cutVarintInt(body)
			if err != nil {
				return nil, fmt.Errorf("decode tick record %d sell level %d volume: %w", i, li+1, err)
			}
			tick.BuyLevels[li] = TickLevel{Price: milliToYuan(qk.priceMilli + buyDelta*10), Volume: buyVol}
			tick.SellLevels[li] = TickLevel{Price: milliToYuan(qk.priceMilli + sellDelta*10), Volume: sellVol}
		}

		if len(body) < 2 {
			return nil, fmt.Errorf("decode tick record %d reserved4: body too short", i)
		}
		body = body[2:]
		body, _, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d reserved5: %w", i, err)
		}
		body, _, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d reserved6: %w", i, err)
		}
		body, _, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d reserved7: %w", i, err)
		}
		body, _, err = cutVarintInt(body)
		if err != nil {
			return nil, fmt.Errorf("decode tick record %d reserved8: %w", i, err)
		}

		if len(body) < 4 {
			return nil, fmt.Errorf("decode tick record %d tail too short: %d", i, len(body))
		}
		tick.ChangeRate = float64(binary.LittleEndian.Uint16(body[:2])) / 100
		tick.Active2 = binary.LittleEndian.Uint16(body[2:4])
		body = body[4:]

		ticks = append(ticks, tick)
	}
	return ticks, nil
}

type tickQuoteK struct {
	prevCloseMilli int64
	openMilli      int64
	highMilli      int64
	lowMilli       int64
	priceMilli     int64
}

func decodeTickQuoteK(body []byte) ([]byte, tickQuoteK, error) {
	var err error
	var closeBase, prevCloseDelta, openDelta, highDelta, lowDelta int64
	body, closeBase, err = cutPrice(body)
	if err != nil {
		return nil, tickQuoteK{}, err
	}
	body, prevCloseDelta, err = cutPrice(body)
	if err != nil {
		return nil, tickQuoteK{}, err
	}
	body, openDelta, err = cutPrice(body)
	if err != nil {
		return nil, tickQuoteK{}, err
	}
	body, highDelta, err = cutPrice(body)
	if err != nil {
		return nil, tickQuoteK{}, err
	}
	body, lowDelta, err = cutPrice(body)
	if err != nil {
		return nil, tickQuoteK{}, err
	}
	priceMilli := closeBase * 10
	return body, tickQuoteK{
		priceMilli:     priceMilli,
		prevCloseMilli: (closeBase + prevCloseDelta) * 10,
		openMilli:      (closeBase + openDelta) * 10,
		highMilli:      (closeBase + highDelta) * 10,
		lowMilli:       (closeBase + lowDelta) * 10,
	}, nil
}
