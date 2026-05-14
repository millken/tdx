package tdx

import (
	"encoding/binary"
	"fmt"
	"time"
)

// XdXr event type constants.
const (
	XdXrChuQuanChuXi         uint8 = 1  // 除权除息
	XdXrSongPeiGuShangShi    uint8 = 2  // 送配股上市
	XdXrFeiLiuTongGuShangShi uint8 = 3  // 非流通股上市
	XdXrGuBenBianDong        uint8 = 4  // 未知股本变动
	XdXrGuBenBianHua         uint8 = 5  // 股本变化
	XdXrZengFaXinGu          uint8 = 6  // 增发新股
	XdXrGuFenHuiGou          uint8 = 7  // 股份回购
	XdXrZengFaShangShi       uint8 = 8  // 增发新股上市
	XdXrZhuanPeiGuShangShi   uint8 = 9  // 转配股上市
	XdXrKeZhuanZhaiShangShi  uint8 = 10 // 可转债上市
	XdXrKuoSuoGu             uint8 = 11 // 扩缩股
	XdXrFeiLiuTongGuSuoGu    uint8 = 12 // 非流通股缩股
	XdXrSongRenGouQuanZheng  uint8 = 13 // 送认购权证
	XdXrSongRenGuQuanZheng   uint8 = 14 // 送认沽权证
)

// XdXrCategoryString returns a Chinese description for an XDXR category code.
func XdXrCategoryString(cat uint8) string {
	switch cat {
	case XdXrChuQuanChuXi:
		return "除权除息"
	case XdXrSongPeiGuShangShi:
		return "送配股上市"
	case XdXrFeiLiuTongGuShangShi:
		return "非流通股上市"
	case XdXrGuBenBianDong:
		return "未知股本变动"
	case XdXrGuBenBianHua:
		return "股本变化"
	case XdXrZengFaXinGu:
		return "增发新股"
	case XdXrGuFenHuiGou:
		return "股份回购"
	case XdXrZengFaShangShi:
		return "增发新股上市"
	case XdXrZhuanPeiGuShangShi:
		return "转配股上市"
	case XdXrKeZhuanZhaiShangShi:
		return "可转债上市"
	case XdXrKuoSuoGu:
		return "扩缩股"
	case XdXrFeiLiuTongGuSuoGu:
		return "非流通股缩股"
	case XdXrSongRenGouQuanZheng:
		return "送认购权证"
	case XdXrSongRenGuQuanZheng:
		return "送认沽权证"
	default:
		return "未知"
	}
}

// XdXrItem represents a single ex-rights/ex-dividend event.
type XdXrItem struct {
	Date     time.Time
	Category uint8

	FenHong     float32 // 分红(每股,元) - category 1
	PeiGuJia    float32 // 配股价       - category 1
	SongZhuanGu float32 // 送转股(每10股) - category 1
	PeiGu       float32 // 配股(每10股)  - category 1

	SuoGu float32 // 缩股比例 - category 11/12

	XingQuanJia float32 // 行权价 - category 13/14
	FenShu      float32 // 份数   - category 13/14

	PanQianLiuTong float32 // 盘前流通股本 - category 2-10
	QianZongGuBen  float32 // 前总股本     - category 2-10
	PanHouLiuTong  float32 // 盘后流通股本 - category 2-10
	HouZongGuBen   float32 // 后总股本     - category 2-10
}

// RequestXdXrFrame builds a 0x000F XDXR request frame.
func RequestXdXrFrame(msgID uint32, control byte, market uint16, code string) ([]byte, error) {
	body := make([]byte, 9)
	binary.LittleEndian.PutUint16(body[0:2], 1)
	body[2] = byte(market)
	copy(body[3:9], code)
	return BuildDirectFrame(msgID, control, DirectFrameTypeXdXr, body), nil
}

// GetXdXr retrieves ex-rights/ex-dividend history for the given stock code.
func (c *MainClient) GetXdXr(code string) ([]XdXrItem, error) {
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

	packet, err := RequestXdXrFrame(0x000A0401, 0x01, market, normalizedCode)
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeXdXr, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty xdxr response")
	}

	return DecodeXdXr(response.Body.Decoded)
}

// DecodeXdXr decodes a 0x000F XDXR response body.
//
// Layout: "<HB6sH" (11 bytes header) + count records.
// Each record (29 bytes): "<B6sBIB" (13 bytes) + 16 bytes payload.
func DecodeXdXr(body []byte) ([]XdXrItem, error) {
	if len(body) < 13 {
		return nil, fmt.Errorf("xdxr body too short: %d", len(body))
	}

	// Skip "<HB6sH" header (2+1+6+2 = 11 bytes), read count
	count := int(binary.LittleEndian.Uint16(body[9:11]))
	body = body[11:]

	const recordSize = 29
	items := make([]XdXrItem, 0, count)
	for i := 0; i < count; i++ {
		if len(body) < recordSize {
			break
		}

		// Skip "<B6sB" (1+6+1 = 8 bytes)
		rec := body[8:]

		dateVal := binary.LittleEndian.Uint32(rec[0:4])
		year := int(dateVal / 10000)
		month := int((dateVal % 10000) / 100)
		day := int(dateVal % 100)
		category := rec[4]
		payload := rec[5:21]

		item := XdXrItem{
			Date:     time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local),
			Category: category,
		}

		switch category {
		case XdXrChuQuanChuXi: // 1: 除权除息
			item.FenHong = readFloat32(payload[0:4])
			item.PeiGuJia = readFloat32(payload[4:8])
			item.SongZhuanGu = readFloat32(payload[8:12])
			item.PeiGu = readFloat32(payload[12:16])
		case XdXrKuoSuoGu, XdXrFeiLiuTongGuSuoGu: // 11/12: 扩缩股
			item.SuoGu = readFloat32(payload[8:12])
		case XdXrSongRenGouQuanZheng, XdXrSongRenGuQuanZheng: // 13/14: 权证
			item.XingQuanJia = readFloat32(payload[0:4])
			item.FenShu = readFloat32(payload[8:12])
		default: // 2-10: 股本变动
			item.PanQianLiuTong = readFloat32(payload[0:4])
			item.QianZongGuBen = readFloat32(payload[4:8])
			item.PanHouLiuTong = readFloat32(payload[8:12])
			item.HouZongGuBen = readFloat32(payload[12:16])
		}

		items = append(items, item)
		body = body[recordSize:]
	}

	return items, nil
}
