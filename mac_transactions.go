package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// MACTransaction 表示 0x122F MAC 分时成交的单条记录。
type MACTransaction struct {
	Time       string  // 成交时刻 "HH:MM:SS"
	Price      float64 // 成交价
	Vol        uint32  // 成交量 (股)
	TradeCount uint32  // 成交笔数
	BuyOrSell  uint16  // 主动方向 (1=买 2=卖)
}

// RequestMACTransactionsFrame 构建 0x122F MAC 分时成交请求帧。
//
// 请求体布局 (44 字节):
//
//	[0:2]   market    uint16 市场代码
//	[2:24]  code      [22]byte 股票代码
//	[24:28] queryDate uint32 查询日期 YYYYMMDD
//	[28:32] start     uint32 起始偏移
//	[32:34] count     uint16 请求数量
//	[34:44] 保留字段全 0
func RequestMACTransactionsFrame(msgID uint32, control byte, market uint16, code22 []byte, queryDate uint32, start uint32, count uint16) ([]byte, error) {
	body := make([]byte, 44)
	binary.LittleEndian.PutUint16(body[0:2], market)
	copy(body[2:24], code22)
	binary.LittleEndian.PutUint32(body[24:28], queryDate)
	binary.LittleEndian.PutUint32(body[28:32], start)
	binary.LittleEndian.PutUint16(body[32:34], count)
	return BuildSPFrame(0x01, DirectFrameTypeMACTransactions, body), nil
}

// GetMACTransactions 获取指定日期的分时成交数据（自动分页）。
//
// 参数:
//   - code: 股票代码，支持 "600000"/"sh600000" 等格式
//   - date: 交易日期 "20060102" 或 "2006-01-02"，空表示当天
//   - count: 最大返回条数
//
// 需通过 DialSP/DialSPBest 建立的连接调用。
//
// 示例:
//
//	trades, err := client.GetMACTransactions("sh600000", "20260806", 1000)
func (c *MainClient) GetMACTransactions(code string, date string, count int) ([]MACTransaction, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}
	if count <= 0 {
		return nil, fmt.Errorf("tdx: mac transactions count must be positive, got %d", count)
	}

	normalizedCode, market, err := macResolveCode(code)
	if err != nil {
		return nil, err
	}
	code22, err := macCode22(normalizedCode)
	if err != nil {
		return nil, err
	}
	queryDate, err := parseDateUint32(date)
	if err != nil {
		return nil, err
	}

	const pageSize = uint16(1000)
	var all []MACTransaction
	start := uint32(0)
	for len(all) < count {
		packet, err := RequestMACTransactionsFrame(0x000A0401, 0x01, market, code22, queryDate, start, pageSize)
		if err != nil {
			return nil, err
		}
		response, err := c.do(packet, DirectFrameTypeMACTransactions, 8*time.Second)
		if err != nil {
			return nil, err
		}
		if response == nil || response.Body == nil {
			break
		}
		items, err := DecodeMACTransactions(response.Body.Decoded)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			break
		}
		all = append(all, items...)
		start += uint32(len(items))
		if len(items) < int(pageSize) {
			break
		}
	}

	if len(all) > count {
		all = all[:count]
	}
	return all, nil
}

// DecodeMACTransactions 解析 0x122F MAC 分时成交响应体。
//
// 响应布局:
//
//	[0:2]   market    uint16
//	[2:24]  code      GBK 22 字节
//	[24:28] queryDate uint32
//	[29:31] count     uint16
//	[31:35] start     uint32
//	[35:39] total     uint32
//	随后 count 条成交，每条 18 字节:
//	  [0:4]   seconds    uint32 → "HH:MM:SS"
//	  [4:8]   price      float32
//	  [8:12]  vol        uint32
//	  [12:16] tradeCount uint32
//	  [16:18] buyOrSell  uint16
func DecodeMACTransactions(body []byte) ([]MACTransaction, error) {
	if len(body) < 39 {
		return nil, fmt.Errorf("mac transactions body too short: %d", len(body))
	}
	count := int(binary.LittleEndian.Uint16(body[29:31]))

	items := make([]MACTransaction, 0, count)
	pos := 39
	for i := 0; i < count; i++ {
		if pos+18 > len(body) {
			break
		}
		seconds := binary.LittleEndian.Uint32(body[pos : pos+4])
		items = append(items, MACTransaction{
			Time:       fmt.Sprintf("%02d:%02d:%02d", (seconds/3600)%24, (seconds%3600)/60, seconds%60),
			Price:      float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+4 : pos+8]))),
			Vol:        binary.LittleEndian.Uint32(body[pos+8 : pos+12]),
			TradeCount: binary.LittleEndian.Uint32(body[pos+12 : pos+16]),
			BuyOrSell:  binary.LittleEndian.Uint16(body[pos+16 : pos+18]),
		})
		pos += 18
	}
	return items, nil
}

// parseDateUint32 把日期字符串解析为 YYYYMMDD 格式的 uint32，空字符串返回当天。
func parseDateUint32(date string) (uint32, error) {
	if date == "" {
		return uint32(time.Now().Year()*10000 + int(time.Now().Month())*100 + time.Now().Day()), nil
	}
	t, err := parseDate(date)
	if err != nil {
		return 0, err
	}
	return uint32(t.Year()*10000 + int(t.Month())*100 + t.Day()), nil
}
