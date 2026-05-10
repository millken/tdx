package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// QuotesList sort type constants.
const (
	SortCode          uint16 = 0x00  // 代码
	SortPrice         uint16 = 0x06  // 现价
	SortVolume        uint16 = 0x09  // 总量
	SortAmount        uint16 = 0x0A  // 总金额
	SortChangePct     uint16 = 0x0E  // 涨幅%
	SortAmplitudePct  uint16 = 0x0F  // 振幅%
	SortPEDynamic     uint16 = 0x11  // 市盈(动)
	SortEntrustRatio  uint16 = 0x12  // 委比%
	SortInOutRatio    uint16 = 0x15  // 内外比
	SortLockedRatio   uint16 = 0x1B  // 封成比
	SortLockedAmount  uint16 = 0x1C  // 封单额
	SortOpenAmount    uint16 = 0x1D  // 开盘金额
	SortVolRatio      uint16 = 0x23  // 量比
	SortTurnoverRate  uint16 = 0x24  // 换手率%
	SortFloatMcap     uint16 = 0x26  // 流通市值
	SortTotalMcapAB   uint16 = 0x27  // AB股总市值
	SortStrengthPct   uint16 = 0x2D  // 强弱度%
	SortSpeedPct      uint16 = 0x2E  // 涨速%
	SortActivity      uint16 = 0x2F  // 活跃度
	SortShortTurnover uint16 = 0xCC  // 短换手%
	SortVolSpeedPct   uint16 = 0xD0  // 量涨速%
	SortMainNetAmount uint16 = 0xD4  // 主力净额
	SortAmount2M      uint16 = 0x10C // 2分钟金额
)

// Client-facing aliases for the board/quotes sort fields.
const (
	SortChange3dPct uint16 = SortStrengthPct // 客户端“3日涨幅”排序
)

// QuotesList filter type constants (exclude bitmask, OR to combine).
// Set bits to EXCLUDE those stock types from results.
const (
	FilterNew uint16 = 1 << 0 // 排除未开板次新股
	FilterKCB uint16 = 1 << 1 // 排除科创板
	FilterST  uint16 = 1 << 2 // 排除ST股
	FilterCYB uint16 = 1 << 3 // 排除创业板
	FilterBJ  uint16 = 1 << 4 // 排除北证A股
)

// QuotesList category constants.
const (
	CategorySH       uint16 = 0      // 上证A
	CategorySZ       uint16 = 2      // 深证A
	CategoryA        uint16 = 6      // A股
	CategoryB        uint16 = 7      // B股
	CategoryKCB      uint16 = 8      // 科创板
	CategoryBJ       uint16 = 12     // 北证A
	CategoryCYB      uint16 = 14     // 创业板
	CategoryHGT      uint16 = 0x2AF9 // 沪股通
	CategorySGT      uint16 = 0x2B01 // 深股通
	CategoryETF      uint16 = 0x2AFD // ETF基金
	CategoryLOF      uint16 = 0x2B04 // LOF基金
	CategoryZS       uint16 = 0x2B2C // 沪深系列指数
	CategoryBoardHY  uint16 = 10001  // 行业一级
	CategoryBoardHY2 uint16 = 10002  // 行业二级
	CategoryBoardGN  uint16 = 10004  // 概念
	CategoryBoardFG  uint16 = 10005  // 风格
	CategoryBoardDQ  uint16 = 10006  // 地区
)

// QuotesList sort order constants.
const (
	SortNone uint16 = 0
	SortDesc uint16 = 1
	SortAsc  uint16 = 2
)

// QuotesItem represents a single stock in the sorted quotes list.
type QuotesItem struct {
	Market        uint16
	Code          string
	Price         float64 // 现价 (元)
	Open          float64 // 开盘价 (元)
	High          float64 // 最高价 (元)
	Low           float64 // 最低价 (元)
	PreClose      float64 // 昨收价 (元)
	ServerTime    string
	Volume        int64   // 总量 (手)
	CurVol        int64   // 现量 (手)
	Amount        float64 // 总金额 (元)
	InVol         int64   // 内盘 (手)
	OutVol        int64   // 外盘 (手)
	RiseSpeed     float64 // 涨速 (%)
	ShortTurnover float32 // 短换手 (%)
	Min2Amount    float32 // 2分钟金额
	VolRatio      float32 // 量比
	Depth         float32 // 委比深度/买卖盘深度
	Active        uint16  // 活跃度
}

// RequestQuotesListFrame builds a 0x054B quotes list request frame.
func RequestQuotesListFrame(msgID uint32, control byte, category uint16, sortType uint16, start uint16, count uint16, sortReverse uint16, filter uint16) ([]byte, error) {
	body := make([]byte, 18)
	binary.LittleEndian.PutUint16(body[0:2], category)
	binary.LittleEndian.PutUint16(body[2:4], sortType)
	binary.LittleEndian.PutUint16(body[4:6], start)
	binary.LittleEndian.PutUint16(body[6:8], count)
	binary.LittleEndian.PutUint16(body[8:10], sortReverse)
	binary.LittleEndian.PutUint16(body[10:12], 5)
	binary.LittleEndian.PutUint16(body[12:14], filter)
	binary.LittleEndian.PutUint16(body[14:16], 1)
	binary.LittleEndian.PutUint16(body[16:18], 0)
	return BuildDirectFrame(msgID, control, DirectFrameTypeQuotesList, body), nil
}

// GetQuotesList retrieves a sorted quotes list for the given market category.
// The optional exclude parameter is a bitmask to exclude stock types (OR multiple FilterXxx constants).
func (c *MainClient) GetQuotesList(category uint16, sortType uint16, start int, count int, reverse bool, exclude ...uint16) ([]QuotesItem, error) {
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

	var excludeMask uint16
	clientFilterBJ := false
	for _, f := range exclude {
		if f == FilterBJ {
			clientFilterBJ = true
			continue // server returns empty when BJ filter bit is set
		}
		excludeMask |= f
	}

	// Request extra items when client-side BJ filtering is needed.
	reqCount := count
	if clientFilterBJ {
		reqCount = count + 200
	}

	packet, err := RequestQuotesListFrame(0x000A0401, 0x01, category, sortType, uint16(start), uint16(reqCount), sortReverse, excludeMask)
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

	items, err := DecodeQuotesList(response.Body.Decoded)
	if err != nil {
		return nil, err
	}

	if clientFilterBJ {
		filtered := make([]QuotesItem, 0, len(items))
		for _, item := range items {
			if item.Market != MarketBeijing {
				filtered = append(filtered, item)
			}
		}
		if len(filtered) > count {
			filtered = filtered[:count]
		}
		return filtered, nil
	}

	return items, nil
}

// DecodeQuotesList decodes a 0x054B quotes list response body.
//
// Header: <HH> (block + count), 4 bytes.
// Per row (variable length due to get_price varint encoding):
//
//	<B6sH> (9 bytes) — market, code, active1
//	9 get_price() varints: price, pre_close, open, high, low, server_time, neg_price, vol, cur_vol
//	<f> (4 bytes) — amount (float32)
//	4 get_price() varints: in_vol, out_vol, s_amount, open_amount
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
		var serverTime int64
		var vol, curVol int64
		var inVol, outVol int64
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

		body, serverTime, err = cutPrice(body)
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

		body, inVol, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("quotes list record %d in_vol: %w", i, err)
		}
		body, outVol, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("quotes list record %d out_vol: %w", i, err)
		}
		for j := 0; j < 2; j++ {
			body, _, err = cutPrice(body)
			if err != nil {
				return nil, fmt.Errorf("quotes list record %d field_%d: %w", i, j+2, err)
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
		// Offsets mirror 0x054C batch quote tail.
		riseSpeed := float64(int16(binary.LittleEndian.Uint16(body[2:4]))) / 100
		shortTurnover := float32(int16(binary.LittleEndian.Uint16(body[4:6]))) / 100
		min2Amount := math.Float32frombits(binary.LittleEndian.Uint32(body[6:10]))
		volRatio := math.Float32frombits(binary.LittleEndian.Uint32(body[22:26]))
		depth := math.Float32frombits(binary.LittleEndian.Uint32(body[26:30]))
		body = body[56:]

		items = append(items, QuotesItem{
			Market:        market,
			Code:          code,
			Price:         float64(price) / 100,
			Open:          float64(open+price) / 100,
			High:          float64(high+price) / 100,
			Low:           float64(low+price) / 100,
			PreClose:      float64(preClose+price) / 100,
			ServerTime:    formatQuoteTime(serverTime),
			Volume:        vol,
			CurVol:        curVol,
			Amount:        float64(amount),
			InVol:         inVol,
			OutVol:        outVol,
			RiseSpeed:     riseSpeed,
			ShortTurnover: shortTurnover,
			Min2Amount:    min2Amount,
			VolRatio:      volRatio,
			Depth:         depth,
			Active:        active,
		})
	}

	return items, nil
}
