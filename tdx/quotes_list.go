package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// QuotesList sort type constants.
const (
	SortCode        uint16 = 0x00
	SortChangePct   uint16 = 0x0E // 涨幅%
	SortAmplitudePct uint16 = 0x0F // 振幅%
	SortVolRatio    uint16 = 0x23 // 量比
	SortTurnoverRate uint16 = 0x24 // 换手率%
	SortSpeedPct    uint16 = 0x2E // 涨速%
)

// QuotesList sort order constants.
const (
	SortNone uint16 = 0
	SortDesc uint16 = 1
	SortAsc  uint16 = 2
)

// QuotesItem represents a single stock in the sorted quotes list.
type QuotesItem struct {
	Market   uint16
	Code     string
	Price    float64 // 现价 (元)
	Open     float64 // 开盘价 (元)
	High     float64 // 最高价 (元)
	Low      float64 // 最低价 (元)
	PreClose float64 // 昨收价 (元)
	Volume   int64   // 总量 (手)
	CurVol   int64   // 现量 (手)
	Amount   float64 // 总金额 (元)
	InVol      int64   // 内盘 (手)
	OutVol     int64   // 外盘 (手)
	RiseSpeed  float64 // 涨速 (%)
	Active     uint16  // 活跃度
}

// RequestQuotesListFrame builds a 0x054B quotes list request frame.
func RequestQuotesListFrame(msgID uint32, control byte, category uint16, sortType uint16, start uint16, count uint16, sortReverse uint16) ([]byte, error) {
	body := make([]byte, 18)
	binary.LittleEndian.PutUint16(body[0:2], category)
	binary.LittleEndian.PutUint16(body[2:4], sortType)
	binary.LittleEndian.PutUint16(body[4:6], start)
	binary.LittleEndian.PutUint16(body[6:8], count)
	binary.LittleEndian.PutUint16(body[8:10], sortReverse)
	binary.LittleEndian.PutUint16(body[10:12], 5)
	binary.LittleEndian.PutUint16(body[12:14], 0)
	binary.LittleEndian.PutUint16(body[14:16], 1)
	binary.LittleEndian.PutUint16(body[16:18], 0)
	return BuildDirectFrame(msgID, control, DirectFrameTypeQuotesList, body), nil
}

// GetQuotesList retrieves a sorted quotes list for the given market category.
func (c *Client) GetQuotesList(category uint16, sortType uint16, start int, count int, reverse bool) ([]QuotesItem, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	c.drainPending()

	sortReverse := SortDesc
	if sortType == SortCode {
		sortReverse = SortNone
	} else if reverse {
		sortReverse = SortAsc
	}

	packet, err := RequestQuotesListFrame(0x000A0401, 0x01, category, sortType, uint16(start), uint16(count), sortReverse)
	if err != nil {
		return nil, err
	}
	if err := c.sendRaw(packet); err != nil {
		return nil, err
	}

	response, err := c.waitForCMD(DirectFrameTypeQuotesList, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty quotes list response")
	}

	return DecodeQuotesList(response.Body.Decoded)
}

// DecodeQuotesList decodes a 0x054B quotes list response body.
//
// Header: <HH> (block + count), 4 bytes.
// Per row (variable length due to get_price varint encoding):
//
//	<B6sH> (9 bytes) — market, code, active1
//	9 get_price() varints: price, pre_close, open, high, low, server_time, neg_price, vol, cur_vol
//	<f> (4 bytes) — amount (float32)
//	3 get_price() varints: in_vol, out_vol, s_amount, open_amount
//	4 get_price() varints: bid, ask, bid_vol, ask_vol
//	<Hhhfh10sff24sH> (56 bytes) — trailing fixed fields
//
// Post-processing: prices /= 100 (厘→元), open/high/low/pre_close += price then /= 100.
func DecodeQuotesList(body []byte) ([]QuotesItem, error) {
	if len(body) < 4 {
		return nil, fmt.Errorf("quotes list body too short: %d", len(body))
	}

	count := int(binary.LittleEndian.Uint16(body[2:4]))
	body = body[4:]

	items := make([]QuotesItem, 0, count)
	for i := 0; i < count; i++ {
		if len(body) < 9 {
			break
		}

		market := uint16(body[0])
		code := trimNulASCII(body[1:7])
		active := binary.LittleEndian.Uint16(body[7:9])
		body = body[9:]

		var price, preClose, open, high, low int64
		var vol, curVol int64
		var err error

		body, price, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("quotes list record %d price: %w", i, err)
		}
		body, preClose, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("quotes list record %d pre_close: %w", i, err)
		}
		body, open, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("quotes list record %d open: %w", i, err)
		}
		body, high, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("quotes list record %d high: %w", i, err)
		}
		body, low, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("quotes list record %d low: %w", i, err)
		}

		// server_time, neg_price
		body, _, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("quotes list record %d server_time: %w", i, err)
		}
		body, _, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("quotes list record %d neg_price: %w", i, err)
		}

		body, vol, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("quotes list record %d vol: %w", i, err)
		}
		body, curVol, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("quotes list record %d cur_vol: %w", i, err)
		}

		// amount (float32, 4 bytes)
		if len(body) < 4 {
			return nil, fmt.Errorf("quotes list record %d amount: unexpected EOF", i)
		}
		amount := math.Float32frombits(binary.LittleEndian.Uint32(body[:4]))
		body = body[4:]

		// in_vol, out_vol, s_amount, open_amount — skip
		for j := 0; j < 4; j++ {
			body, _, err = cutPrice(body)
			if err != nil {
				return nil, fmt.Errorf("quotes list record %d field_%d: %w", i, j, err)
			}
		}

		// bid, ask, bid_vol, ask_vol — skip
		for j := 0; j < 4; j++ {
			body, _, err = cutPrice(body)
			if err != nil {
				return nil, fmt.Errorf("quotes list record %d bid/ask_%d: %w", i, j, err)
			}
		}

		// 56-byte trailing fixed fields
		if len(body) < 56 {
			return nil, fmt.Errorf("quotes list record %d tail: unexpected EOF", i)
		}
		// rise_speed at offset 2 (int16)
		riseSpeed := float64(int16(binary.LittleEndian.Uint16(body[2:4]))) / 100
		body = body[56:]

		items = append(items, QuotesItem{
			Market:   market,
			Code:     code,
			Price:    float64(price) / 100,
			Open:     float64(open+price) / 100,
			High:     float64(high+price) / 100,
			Low:      float64(low+price) / 100,
			PreClose: float64(preClose+price) / 100,
			Volume:   vol,
			CurVol:   curVol,
			Amount:   float64(amount),
			Active:   active,
			RiseSpeed: riseSpeed,
		})
	}

	return items, nil
}
