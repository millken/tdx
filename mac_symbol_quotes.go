package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// RequestMACSymbolQuotesFrame 构建 0x122B MAC 批量股票报价请求帧。
//
// 请求体布局:
//
//	[0:20]                       fieldBitmap [20]byte 字段位图
//	[20:22]                      stockCount  uint16 股票数量
//	随后 stockCount 个目标，每个 24 字节:
//	  [0:2]   market uint16
//	  [2:24]  code   [22]byte
func RequestMACSymbolQuotesFrame(msgID uint32, control byte, bitmap []byte, stocks []MACStock) ([]byte, error) {
	body := make([]byte, 0, 22+24*len(stocks))
	body = append(body, bitmap...)
	body = binary.LittleEndian.AppendUint16(body, uint16(len(stocks)))
	for _, s := range stocks {
		body = binary.LittleEndian.AppendUint16(body, s.Market)
		buf := make([]byte, 22)
		copy(buf, s.Code)
		body = append(body, buf...)
	}
	return BuildSPFrame(0x01, DirectFrameTypeMACSymbolQuotes, body), nil
}

// MACStock 表示 MAC 批量报价的单个查询目标。
type MACStock struct {
	Market uint16 // 市场代码 (0=深 1=沪 2=北)
	Code   string // 6 位股票代码
}

// GetMACSymbolQuotes 获取多只股票的批量报价（字段集由 bitmap 决定）。
//
// 参数:
//   - codes: 股票代码列表，支持 "600000"/"sh600000" 等格式
//   - bitmap: 20 字节字段位图，nil 时使用 defaultBitmap
//
// 返回的 BoardMembersItem 复用板块成员报价结构，未在 bitmap 中请求的字段为零值。
// 需通过 DialSP/DialSPBest 建立的连接调用。
//
// 示例:
//
//	items, err := client.GetMACSymbolQuotes([]string{"sh600000", "sz000001"}, nil)
func (c *MainClient) GetMACSymbolQuotes(codes []string, bitmap []byte) ([]BoardMembersItem, error) {
	if len(codes) == 0 {
		return nil, fmt.Errorf("tdx: mac symbol quotes requires at least one code")
	}
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	if bitmap == nil {
		bitmap = defaultBitmap
	}

	stocks := make([]MACStock, 0, len(codes))
	for _, code := range codes {
		normalizedCode, market, err := macResolveCode(code)
		if err != nil {
			return nil, err
		}
		stocks = append(stocks, MACStock{Market: market, Code: normalizedCode})
	}

	packet, err := RequestMACSymbolQuotesFrame(0x000A0401, 0x01, bitmap, stocks)
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeMACSymbolQuotes, 8*time.Second)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty mac symbol quotes response")
	}

	return DecodeMACSymbolQuotes(response.Body.Decoded)
}

// DecodeMACSymbolQuotes 解析 0x122B MAC 批量股票报价响应体。
//
// 响应布局:
//
//	[0:20]   fieldBitmap [20]byte 服务器回显的字段位图
//	[20:24]  total       uint32
//	[24:26]  count       uint16 返回行数
//	随后 count 行，每行长度 = 68 + len(activeBits)*4:
//	  [0:2]   market uint16
//	  [2:24]  code   GBK 22 字节
//	  [24:68] name   GBK 44 字节
//	  [68+]   每个 active bit 对应 4 字节字段值 (float32/uint32/int32)
//
// 字段位图、active bits、字段类型与 board_members (0x122C 报价变体) 完全一致，
// 故复用 getActiveBits / boardMembersFieldFmt / Bitmap* 位常量。
func DecodeMACSymbolQuotes(body []byte) ([]BoardMembersItem, error) {
	if len(body) < 26 {
		return nil, fmt.Errorf("mac symbol quotes body too short: %d", len(body))
	}

	bitmapEcho := body[:20]
	rowCount := int(binary.LittleEndian.Uint16(body[24:26]))
	activeBits := getActiveBits(bitmapEcho)
	rowLen := 68 + 4*len(activeBits)
	body = body[26:]

	if rowLen == 0 {
		return nil, fmt.Errorf("mac symbol quotes: no active bitmap fields")
	}
	if len(body) < rowCount*rowLen {
		rowCount = len(body) / rowLen
	}

	items := make([]BoardMembersItem, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		row := body[i*rowLen : (i+1)*rowLen]
		item := BoardMembersItem{
			Market: binary.LittleEndian.Uint16(row[0:2]),
			Code:   trimNulCodeGBK(row[2:24]),
			Name:   trimNulCodeGBK(row[24:68]),
		}
		decodeMACDynamicRow(&item, row[68:], activeBits)
		items = append(items, item)
	}
	return items, nil
}

// decodeMACDynamicRow 将 bitmap 动态字段值解码进 BoardMembersItem。
// offset 指向首行 68 字节固定头之后的动态字段区。
func decodeMACDynamicRow(item *BoardMembersItem, fields []byte, activeBits []int) {
	offset := 0
	for _, bit := range activeBits {
		if offset+4 > len(fields) {
			break
		}
		raw := binary.LittleEndian.Uint32(fields[offset : offset+4])
		val := math.Float32frombits(raw)
		offset += 4

		switch bit {
		case BitmapPreClose:
			item.PreClose = val
		case BitmapOpen:
			item.Open = val
		case BitmapHigh:
			item.High = val
		case BitmapLow:
			item.Low = val
		case BitmapClose:
			item.Close = val
		case BitmapVol:
			item.Volume = raw
		case BitmapVolRatio:
			item.VolRatio = val
		case BitmapAmount:
			item.Amount = val
		case BitmapInsideVol:
			item.InsideVol = raw
		case BitmapOutsideVol:
			item.OutsideVol = raw
		case BitmapTotalShares:
			item.TotalShares = val
		case BitmapFloatShares:
			item.FloatShares = val
		case BitmapEPS:
			item.EPS = val
		case BitmapNetAssets:
			item.NetAssets = val
		case BitmapTotalMcapAB:
			item.TotalMcapAB = val
		case BitmapPEDynamic:
			item.PEDynamic = val
		case BitmapBid:
			item.Bid = val
		case BitmapAsk:
			item.Ask = val
		case BitmapBidVol:
			item.BidVol = raw
		case BitmapAskVol:
			item.AskVol = raw
		case BitmapLastVol:
			item.LastVol = raw
		case BitmapTurnover:
			item.Turnover = val
		case BitmapBuyPriceLimit:
			item.BuyPriceLimit = val
		case BitmapSellPriceLimit:
			item.SellPriceLimit = val
		case BitmapLotSize:
			item.LotSize = raw
		case BitmapSpeedPct:
			item.SpeedPct = val
		case BitmapAvgPrice:
			item.AvgPrice = val
		case BitmapKCBFlag:
			item.KCBFlag = raw
		case BitmapBJFlag:
			item.BJFlag = raw
		case BitmapPETTM:
			item.PETTM = val
		case BitmapPEStatic:
			item.PEStatic = val
		case BitmapChange20d:
			item.Change20d = val
		case BitmapYtdPct:
			item.YtdPct = val
		case BitmapChange60d:
			item.Change60d = val
		case BitmapChange5d:
			item.Change5d = val
		case BitmapChange10d:
			item.Change10d = val
		case BitmapMainNetAmount:
			item.MainNetAmount = val
		case BitmapActivity:
			item.Activity = raw
		case BitmapConsecutiveUp:
			item.ConsecutiveUp = int32(raw)
		}
	}
}
