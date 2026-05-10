package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"time"
)

// Default bitmap includes the commonly used quote fields plus multi-day change fields,
// Activity(0x59), and ConsecutiveUp(0x5c).
var defaultBitmap = []byte{
	0xff, 0xfc, 0xe1, 0xcc, 0x3f, 0x08, 0x03, 0x00,
	0x70, 0x00, 0x00, 0x00, 0x12, 0x08, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00,
}

// Bitmap field bit positions.
const (
	BitmapPreClose       = 0x00
	BitmapOpen           = 0x01
	BitmapHigh           = 0x02
	BitmapLow            = 0x03
	BitmapClose          = 0x04
	BitmapVol            = 0x05
	BitmapVolRatio       = 0x06
	BitmapAmount         = 0x07
	BitmapInsideVol      = 0x08
	BitmapOutsideVol     = 0x09
	BitmapTotalShares    = 0x0a
	BitmapFloatShares    = 0x0b
	BitmapEPS            = 0x0c
	BitmapNetAssets      = 0x0d
	BitmapTotalMcapAB    = 0x0f
	BitmapPEDynamic      = 0x10
	BitmapBid            = 0x11
	BitmapAsk            = 0x12
	BitmapServerDate     = 0x13
	BitmapServerTime     = 0x14
	BitmapBidVol         = 0x18
	BitmapAskVol         = 0x19
	BitmapLastVol        = 0x1a
	BitmapTurnover       = 0x1b
	BitmapIndustry       = 0x1c
	BitmapDecimalPoint   = 0x1f
	BitmapBuyPriceLimit  = 0x20
	BitmapSellPriceLimit = 0x21
	BitmapLotSize        = 0x23
	BitmapSpeedPct       = 0x25
	BitmapAvgPrice       = 0x26
	BitmapKCBFlag        = 0x2b
	BitmapBJFlag         = 0x2c
	BitmapPETTM          = 0x30
	BitmapPEStatic       = 0x31
	BitmapChange20d      = 0x3b
	BitmapYtdPct         = 0x3c
	BitmapChange60d      = 0x44
	BitmapChange5d       = 0x45
	BitmapChange10d      = 0x46
	BitmapActivity       = 0x59
	BitmapConsecutiveUp  = 0x5c
	BitmapMainNetAmount  = 0x6b
)

// Board members sort type constants used by the 0x122C protocol.
const (
	BoardMembersSortCode          uint16 = SortCode
	BoardMembersSortVolRatio      uint16 = BitmapVolRatio
	BoardMembersSortAmount        uint16 = BitmapAmount
	BoardMembersSortChangePct     uint16 = SortChangePct
	BoardMembersSortTurnover      uint16 = BitmapTurnover
	BoardMembersSortMainNetAmount uint16 = SortMainNetAmount
)

// boardMembersFieldFmt returns 'I' for uint32 fields, 'f' for float32, 'i' for int32.
func boardMembersFieldFmt(bit int) byte {
	switch bit {
	case BitmapVol, BitmapInsideVol, BitmapOutsideVol,
		BitmapServerDate, BitmapServerTime, BitmapBidVol,
		BitmapAskVol, BitmapLastVol, BitmapIndustry,
		BitmapDecimalPoint, BitmapLotSize,
		BitmapKCBFlag, BitmapBJFlag, BitmapActivity:
		return 'I'
	case BitmapConsecutiveUp:
		return 'i'
	default:
		return 'f'
	}
}

// BoardMembersItem represents a single stock from the board members list.
type BoardMembersItem struct {
	Market         uint16
	Code           string
	Name           string
	PreClose       float32
	Open           float32
	High           float32
	Low            float32
	Close          float32
	Volume         uint32
	VolRatio       float32
	Amount         float32
	InsideVol      uint32
	OutsideVol     uint32
	TotalShares    float32
	FloatShares    float32
	EPS            float32
	NetAssets      float32
	TotalMcapAB    float32
	Bid            float32
	Ask            float32
	BidVol         uint32
	AskVol         uint32
	LastVol        uint32
	Turnover       float32
	BuyPriceLimit  float32
	SellPriceLimit float32
	LotSize        uint32
	SpeedPct       float32
	AvgPrice       float32
	KCBFlag        uint32
	BJFlag         uint32
	PETTM          float32
	PEDynamic      float32
	PEStatic       float32
	Change20d      float32
	YtdPct         float32
	Change60d      float32
	Change5d       float32
	Change10d      float32
	MainNetAmount  float32
	Activity       uint32
	ConsecutiveUp  int32
}

// exchangeBoardCode converts a visual board symbol to the internal code.
func exchangeBoardCode(symbol string) uint32 {
	switch {
	case len(symbol) > 2 && symbol[:2] == "US":
		n, _ := strconv.Atoi(symbol[2:])
		return 30000 + uint32(n)
	case len(symbol) > 2 && symbol[:2] == "HK":
		n, _ := strconv.Atoi(symbol[2:])
		return 20000 + uint32(n)
	case len(symbol) == 6 && symbol[:3] == "000":
		n, _ := strconv.Atoi(symbol)
		return 31000 + uint32(n)
	case len(symbol) == 6 && symbol[:3] == "399":
		n, _ := strconv.Atoi(symbol)
		return uint32(n) - 399000 + 30000
	case len(symbol) == 6 && symbol[:3] == "899":
		n, _ := strconv.Atoi(symbol)
		return uint32(n) - 899000 + 32000
	case len(symbol) == 6 && symbol[:2] == "88":
		n, _ := strconv.Atoi(symbol)
		return uint32(n) - 880000 + 20000
	default:
		n, _ := strconv.Atoi(symbol)
		return uint32(n)
	}
}

// RequestBoardMembersFrame builds a 0x122C board members request frame.
func RequestBoardMembersFrame(msgID uint32, control byte, boardCode uint32, sortType uint16, start uint32, pageSize uint8, sortOrder uint16, bitmap []byte) ([]byte, error) {
	body := make([]byte, 0, 43)

	part1 := make([]byte, 13)
	binary.LittleEndian.PutUint32(part1[0:4], boardCode)
	body = append(body, part1...)

	part2 := make([]byte, 10)
	binary.LittleEndian.PutUint16(part2[0:2], sortType)
	binary.LittleEndian.PutUint32(part2[2:6], start)
	part2[6] = pageSize
	part2[7] = 0
	part2[8] = byte(sortOrder)
	part2[9] = 0
	body = append(body, part2...)

	body = append(body, bitmap...)

	return BuildSPFrame(0x01, DirectFrameTypeBoardMembers, body), nil
}

// GetBoardMembers retrieves stock list for a board with auto-pagination.
func (c *MainClient) GetBoardMembers(board string, sortType uint16, count int, sortOrder uint16) ([]BoardMembersItem, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	boardCode := exchangeBoardCode(board)
	var allItems []BoardMembersItem
	start := uint32(0)
	const pageSize = 80

	for len(allItems) < count {
		c.drainPending()

		packet, err := RequestBoardMembersFrame(0x000A0401, 0x01, boardCode, sortType, start, pageSize, sortOrder, defaultBitmap)
		if err != nil {
			return nil, err
		}
		if err := c.sendRaw(packet); err != nil {
			return nil, err
		}

		response, err := c.waitForCMD(DirectFrameTypeBoardMembers, 8*time.Second)
		if err != nil {
			return nil, err
		}
		if response == nil || response.Body == nil {
			break
		}

		items, err := DecodeBoardMembers(response.Body.Decoded)
		if err != nil {
			return nil, err
		}

		if len(items) == 0 {
			break
		}

		allItems = append(allItems, items...)
		start += uint32(len(items))

		if len(items) < pageSize {
			break
		}
	}

	if len(allItems) > count {
		allItems = allItems[:count]
	}
	return allItems, nil
}

// DecodeBoardMembers decodes a 0x122C board members response body.
//
// Header: 20-byte bitmap echo + 4-byte total + 2-byte row_count = 26 bytes.
// Per row: 68-byte fixed header (market:2 + code:22 GBK + name:44 GBK) + 4 bytes per active bit.
func DecodeBoardMembers(body []byte) ([]BoardMembersItem, error) {
	if len(body) < 26 {
		return nil, fmt.Errorf("board members body too short: %d", len(body))
	}

	bitmapEcho := body[:20]
	rowCount := int(binary.LittleEndian.Uint16(body[24:26]))

	activeBits := getActiveBits(bitmapEcho)

	dynamicCount := len(activeBits)
	rowLen := 68 + 4*dynamicCount
	body = body[26:]

	if rowLen == 0 {
		return nil, fmt.Errorf("board members: no active bitmap fields")
	}

	if len(body) < rowCount*rowLen {
		rowCount = len(body) / rowLen
	}

	items := make([]BoardMembersItem, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		row := body[i*rowLen : (i+1)*rowLen]

		market := binary.LittleEndian.Uint16(row[0:2])
		code := trimNulCodeGBK(row[2:24])
		name := trimNulCodeGBK(row[24:68])

		item := BoardMembersItem{
			Market: market,
			Code:   code,
			Name:   name,
		}

		offset := 68
		for _, bit := range activeBits {
			if offset+4 > len(row) {
				break
			}
			raw := binary.LittleEndian.Uint32(row[offset : offset+4])
			val := math.Float32frombits(raw)
			fmtType := boardMembersFieldFmt(bit)
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
				if fmtType == 'i' {
					item.ConsecutiveUp = int32(raw)
				}
			}

			_ = fmtType
		}

		items = append(items, item)
	}

	return items, nil
}

// getActiveBits extracts sorted active bit positions from a 20-byte bitmap.
func getActiveBits(bitmap []byte) []int {
	var bits []int
	for byteIdx := 0; byteIdx < 20; byteIdx++ {
		b := bitmap[byteIdx]
		for bitIdx := 0; bitIdx < 8; bitIdx++ {
			if b&(1<<uint(bitIdx)) != 0 {
				bits = append(bits, byteIdx*8+bitIdx)
			}
		}
	}
	return bits
}

var _ = time.Local
