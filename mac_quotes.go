package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// MACQuote 表示 0x122D MAC 行情快照的完整响应。
//
// 该命令一次返回单只股票的：当日实时摘要（开高低收/量额/换手等）+ 当日分时
// 采样序列（价格、均价、成交量、动量）。相当于主站 GetTick + GetTickChart 的合体。
type MACQuote struct {
	Market       uint16            // 市场代码 (0=深 1=沪 2=北)
	Code         string            // 股票代码
	Name         string            // 股票名称
	Date         uint32            // 交易日期 YYYYMMDD
	DateTime     time.Time         // 服务器时间
	Price        float64           // 最新价
	PreClose     float64           // 昨收价
	Open         float64           // 开盘价
	High         float64           // 最高价
	Low          float64           // 最低价
	Close        float64           // 收盘价
	Momentum     float64           // 涨跌 (最新价 - 昨收)
	Vol          uint32            // 成交量 (股)
	Amount       float64           // 成交额 (元)
	Turnover     float64           // 换手率
	Avg          float64           // 均价
	Decimal      uint8             // 小数位数
	Category     uint16            // 证券类别
	VolUnit      float64           // 成交量单位 (手)
	Industry     uint32            // 行业代码
	IndustryCode string            // 行业板块代码
	ChartData    []MACQuoteChart   // 当日分时采样序列
}

// MACQuoteChart 表示 MAC 行情快照中的单个分时采样点。
type MACQuoteChart struct {
	Time     string  // 采样时刻 "HH:MM:SS"
	Price    float64 // 采样价格
	Avg      float64 // 采样均价
	Vol      uint32  // 采样成交量
	Momentum float64 // 采样动量
}

// RequestMACQuotesFrame 构建 0x122D MAC 行情快照请求帧。
//
// 请求体布局 (38 字节):
//
//	[0:2]   market        uint16 市场代码
//	[2:24]  code          [22]byte 股票代码
//	[24:38] 保留字段全 0，其中 offset 28 处的 uint16 必须为 1
func RequestMACQuotesFrame(msgID uint32, control byte, market uint16, code22 []byte) ([]byte, error) {
	body := make([]byte, 38)
	binary.LittleEndian.PutUint16(body[0:2], market)
	copy(body[2:24], code22)
	binary.LittleEndian.PutUint16(body[28:30], 1) // One 字段固定为 1
	return BuildSPFrame(0x01, DirectFrameTypeMACQuotes, body), nil
}

// GetMACQuotes 获取单只股票的 MAC 行情快照（含当日分时采样）。
// code 支持 "600000"/"sh600000" 等格式。需通过 DialSP/DialSPBest 建立的连接调用。
//
// 示例:
//
//	quote, err := client.GetMACQuotes("sh600000")
func (c *MainClient) GetMACQuotes(code string) (*MACQuote, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	normalizedCode, market, err := macResolveCode(code)
	if err != nil {
		return nil, err
	}
	code22, err := macCode22(normalizedCode)
	if err != nil {
		return nil, err
	}

	packet, err := RequestMACQuotesFrame(0x000A0401, 0x01, market, code22)
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeMACQuotes, 8*time.Second)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty mac quotes response")
	}

	return DecodeMACQuotes(response.Body.Decoded)
}

// DecodeMACQuotes 解析 0x122D MAC 行情快照响应体。
//
// 响应布局:
//
//	[0:2]   market  uint16
//	[2:24]  code    GBK 22 字节
//	[24:28] date    uint32 YYYYMMDD
//	[28]    unknown uint8
//	[29:33] price   float32 最新价
//	[33:35] count   uint16  分时采样点数量
//	随后 count 个采样点，每个 18 字节:
//	  [0:2]   minutes  uint16 → "HH:MM:SS"
//	  [2:6]   price    float32
//	  [6:10]  avg      float32
//	  [10:14] vol      uint32
//	  [14:18] momentum float32
//	采样点之后是 120 字节摘要尾部 (name/decimal/开高低收/量额/换手/均价/行业)。
func DecodeMACQuotes(body []byte) (*MACQuote, error) {
	if len(body) < 35 {
		return nil, fmt.Errorf("mac quotes body too short: %d", len(body))
	}

	q := &MACQuote{
		Market: binary.LittleEndian.Uint16(body[0:2]),
		Code:   trimNulCodeGBK(body[2:24]),
		Date:   binary.LittleEndian.Uint32(body[24:28]),
		Price:  float64(math.Float32frombits(binary.LittleEndian.Uint32(body[29:33]))),
	}
	count := int(binary.LittleEndian.Uint16(body[33:35]))

	pos := 35
	for i := 0; i < count; i++ {
		if pos+18 > len(body) {
			return nil, fmt.Errorf("mac quotes chart item %d out of range", i)
		}
		minutes := binary.LittleEndian.Uint16(body[pos : pos+2])
		q.ChartData = append(q.ChartData, MACQuoteChart{
			Time:     fmt.Sprintf("%02d:%02d:00", (minutes/60)%24, minutes%60),
			Price:    float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+2 : pos+6]))),
			Avg:      float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+6 : pos+10]))),
			Vol:      binary.LittleEndian.Uint32(body[pos+10 : pos+14]),
			Momentum: float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+14 : pos+18]))),
		})
		pos += 18
	}

	if pos+120 > len(body) {
		// 仅返回分时数据，摘要尾部缺失。
		return q, nil
	}

	fillMACSummary120(q, body[pos:pos+120])
	return q, nil
}

// fillMACSummary120 解析 MAC 协议通用的 120 字节摘要尾部。
// 该尾部在 MACQuotes / MACSymbolBars / MACTickCharts 等命令中结构一致。
//
// 布局 (120 字节):
//
//	[0:44]    name        GBK 名称
//	[44]      decimal     uint8 小数位
//	[45:47]   category    uint16 证券类别
//	[47:51]   volUnit     float32 成交量单位
//	[56:60]   date        uint32 YYYYMMDD
//	[60:64]   time        uint32 HHMMSS
//	[64:68]   preClose    float32
//	[68:72]   open        float32
//	[72:76]   high        float32
//	[76:80]   low         float32
//	[80:84]   close       float32
//	[84:88]   momentum    float32
//	[88:92]   vol         uint32
//	[92:96]   amount      float32
//	[108:112] turnover    float32
//	[112:116] avg         float32
//	[116:120] industry    uint32
func fillMACSummary120(q *MACQuote, b []byte) {
	q.Name = trimNulCodeGBK(b[0:44])
	q.Decimal = b[44]
	q.Category = binary.LittleEndian.Uint16(b[45:47])
	q.VolUnit = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[47:51])))
	q.DateTime = macParseDateTime(
		binary.LittleEndian.Uint32(b[56:60]),
		binary.LittleEndian.Uint32(b[60:64]),
	)
	q.PreClose = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[64:68])))
	q.Open = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[68:72])))
	q.High = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[72:76])))
	q.Low = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[76:80])))
	q.Close = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[80:84])))
	q.Momentum = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[84:88])))
	q.Vol = binary.LittleEndian.Uint32(b[88:92])
	q.Amount = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[92:96])))
	q.Turnover = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[108:112])))
	q.Avg = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[112:116])))
	q.Industry = binary.LittleEndian.Uint32(b[116:120])
	q.IndustryCode = macIndustryBoardSymbol(q.Industry)
}
