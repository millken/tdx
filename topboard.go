package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// TopBoard category constants.
const (
	TopBoardSH  uint16 = 0  // 上证A股
	TopBoardSZ  uint16 = 2  // 深证A股
	TopBoardA   uint16 = 6  // 所有A股
	TopBoardB   uint16 = 7  // B股
	TopBoardKCB uint16 = 8  // 科创板
	TopBoardBJ  uint16 = 12 // 北证A股
	TopBoardCYB uint16 = 14 // 创业板
)

// TopBoardItem represents a single entry in a ranking list.
type TopBoardItem struct {
	Market uint16
	Code   string
	Price  float32
	Value  float32
}

// TopBoardResult holds all 9 ranking lists from a single 0x53F response.
type TopBoardResult struct {
	Increase           []TopBoardItem // 涨幅榜
	Decrease           []TopBoardItem // 跌幅榜
	Amplitude          []TopBoardItem // 振幅榜
	RiseSpeed          []TopBoardItem // 涨速榜
	FallSpeed          []TopBoardItem // 跌速榜
	VolRatio           []TopBoardItem // 量比榜
	PosCommissionRatio []TopBoardItem // 委比正序
	NegCommissionRatio []TopBoardItem // 委比倒序
	Turnover           []TopBoardItem // 换手率榜
}

// RequestTopBoardFrame builds a 0x053F top board request frame.
func RequestTopBoardFrame(msgID uint32, control byte, category uint16, size uint8) ([]byte, error) {
	body := make([]byte, 10)
	body[0] = byte(category)
	body[1] = 5
	copy(body[2:9], []byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00})
	body[9] = size
	return BuildDirectFrame(msgID, control, DirectFrameTypeTopBoard, body), nil
}

// GetTopBoard retrieves 9 ranking lists (涨幅/跌幅/振幅/涨速/跌速/量比/委比/换手率).
func (c *MainClient) GetTopBoard(category uint16, size int) (*TopBoardResult, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	packet, err := RequestTopBoardFrame(0x000A0401, 0x01, category, uint8(size))
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeTopBoard, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty top board response")
	}

	return DecodeTopBoard(response.Body.Decoded)
}

// DecodeTopBoard decodes a 0x053F top board response body.
//
// Layout: 1 byte size, then 9 lists × size entries.
// Each entry: <B6sff> = market(1) + code(6) + price(float32) + value(float32) = 15 bytes.
func DecodeTopBoard(body []byte) (*TopBoardResult, error) {
	if len(body) < 1 {
		return nil, fmt.Errorf("top board body too short: %d", len(body))
	}

	size := int(body[0])
	body = body[1:]

	const entrySize = 15
	const listCount = 9
	result := &TopBoardResult{}

	boards := []*[]TopBoardItem{
		&result.Increase, &result.Decrease, &result.Amplitude,
		&result.RiseSpeed, &result.FallSpeed, &result.VolRatio,
		&result.PosCommissionRatio, &result.NegCommissionRatio, &result.Turnover,
	}

	for _, board := range boards {
		items := make([]TopBoardItem, 0, size)
		for i := 0; i < size; i++ {
			if len(body) < entrySize {
				break
			}
			market := uint16(body[0])
			code := trimNulASCII(body[1:7])
			price := math.Float32frombits(binary.LittleEndian.Uint32(body[7:11]))
			value := math.Float32frombits(binary.LittleEndian.Uint32(body[11:15]))
			items = append(items, TopBoardItem{
				Market: market,
				Code:   code,
				Price:  price,
				Value:  value,
			})
			body = body[entrySize:]
		}
		*board = items
	}

	return result, nil
}
