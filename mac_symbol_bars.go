package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"
)

// MACSymbolBar 表示 0x122E MAC 统一K线的单根 K 线。
//
// 相比主站 GetKline，MAC 统一K线支持前复权 (adjust 参数)，并额外返回流通股本、
// 涨跌价/涨跌幅等派生字段。
type MACSymbolBar struct {
	DateTime    time.Time // K 线时间
	Open        float64   // 开盘价
	High        float64   // 最高价
	Low         float64   // 最低价
	Close       float64   // 收盘价
	Amount      float64   // 成交额 (元)
	Vol         float64   // 成交量
	FloatShares float64   // 流通股本
	PreClose    float64   // 昨收价 (上一根收盘)
	RisePrice   float64   // 涨跌价 (close - preclose)
	RiseRate    float64   // 涨跌幅 (百分比)
}

// MACSymbolBars 表示 0x122E MAC 统一K线响应（含 K 线序列 + 摘要尾部）。
type MACSymbolBars struct {
	Market       uint16          // 市场代码
	Code         string          // 股票代码
	Name         string          // 股票名称
	Period       uint8           // 周期
	Count        uint16          // K 线数量
	Start        uint32          // 起始偏移
	List         []MACSymbolBar  // K 线序列
	PreClose     float64         // 摘要昨收
	Open         float64         // 摘要开盘
	High         float64         // 摘要最高
	Low          float64         // 摘要最低
	Close        float64         // 摘要最新
	Vol          uint32          // 摘要成交量
	Amount       float64         // 摘要成交额
	Turnover     float64         // 摘要换手率
	Avg          float64         // 摘要均价
	DateTime     time.Time       // 摘要服务器时间
	Decimal      uint8           // 小数位
	Category     uint16          // 证券类别
	VolUnit      float64         // 成交量单位
	Industry     uint32          // 行业代码
	IndustryCode string          // 行业板块代码
}

// MAC 复权方式常量。
const (
	MACAdjustNone uint16 = 0 // 不复权
	MACAdjustQFQ  uint16 = 1 // 前复权
	MACAdjustHFQ  uint16 = 2 // 后复权
)

// RequestMACSymbolBarsFrame 构建 0x122E MAC 统一K线请求帧。
//
// 请求体布局 (46 字节):
//
//	[0:2]   market   uint16 市场代码
//	[2:24]  code     [22]byte 股票代码
//	[24:26] period   uint16 周期
//	[26:28] times    uint16 固定为 1
//	[28:32] start    uint32 起始偏移
//	[32:34] count    uint16 请求数量
//	[34:36] adjust   uint16 复权方式
//	[36]    flag1    int8 固定为 1
//	[37]    flag2    int8 固定为 1
//	[38]    flag3    int8
//	[39]    flag4    int8 固定为 1
//	[40:42] 保留
//	[42:46] 保留
func RequestMACSymbolBarsFrame(msgID uint32, control byte, market uint16, code22 []byte, period uint16, times uint16, start uint32, count uint16, adjust uint16) ([]byte, error) {
	body := make([]byte, 46)
	binary.LittleEndian.PutUint16(body[0:2], market)
	copy(body[2:24], code22)
	binary.LittleEndian.PutUint16(body[24:26], period)
	binary.LittleEndian.PutUint16(body[26:28], times)
	binary.LittleEndian.PutUint32(body[28:32], start)
	binary.LittleEndian.PutUint16(body[32:34], count)
	binary.LittleEndian.PutUint16(body[34:36], adjust)
	body[36] = 1 // flag1
	body[37] = 1 // flag2
	body[39] = 1 // flag4
	return BuildSPFrame(0x01, DirectFrameTypeMACSymbolBars, body), nil
}

// GetMACSymbolBars 获取 MAC 统一K线数据。
//
// 参数:
//   - code: 股票代码，支持 "600000"/"sh600000" 等格式
//   - period: 周期，如 "day"/"1m"/"5m"/"60m"/"week"/"month"（同 GetKline）
//   - count: K 线数量
//   - adjust: 复权方式，0=不复权 1=前复权 2=后复权
//
// 需通过 DialSP/DialSPBest 建立的连接调用。
//
// 示例:
//
//	bars, err := client.GetMACSymbolBars("sh600000", "day", 100, tdx.MACAdjustQFQ)
func (c *MainClient) GetMACSymbolBars(code string, period string, count int, adjust uint16) (*MACSymbolBars, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}
	if count <= 0 {
		return nil, fmt.Errorf("tdx: mac symbol bars count must be positive, got %d", count)
	}

	normalizedCode, market, err := macResolveCode(code)
	if err != nil {
		return nil, err
	}
	code22, err := macCode22(normalizedCode)
	if err != nil {
		return nil, err
	}

	pv, ok := periodMap[strings.ToLower(period)]
	if !ok {
		return nil, fmt.Errorf("tdx: unsupported period %q", period)
	}

	// 协议要求 count+1：返回的第一根 K 线仅作为 preClose 种子，不放入结果。
	packet, err := RequestMACSymbolBarsFrame(0x000A0401, 0x01, market, code22, pv.period, pv.times, 0, uint16(count+1), adjust)
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeMACSymbolBars, 8*time.Second)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty mac symbol bars response")
	}

	return DecodeMACSymbolBars(response.Body.Decoded)
}

// DecodeMACSymbolBars 解析 0x122E MAC 统一K线响应体。
//
// 响应布局:
//
//	[0:2]   market   uint16
//	[2:14]  code     GBK 12 字节
//	[24]    period   uint8
//	[27:29] count    uint16 (含被丢弃的种子 bar)
//	[29:33] start    uint32
//	随后 count 个 K 线，每个 36 字节:
//	  [0:4]   ymd         uint32 YYYYMMDD
//	  [4:8]   seconds     uint32 → 时分
//	  [8:12]  open        float32
//	  [12:16] high        float32
//	  [16:20] low         float32
//	  [20:24] close       float32
//	  [24:28] amount      float32
//	  [28:32] vol         float32
//	  [32:36] floatShares float32
//	K 线之后是 120 字节摘要尾部 (与 MACQuotes 一致)。
func DecodeMACSymbolBars(body []byte) (*MACSymbolBars, error) {
	if len(body) < 33 {
		return nil, fmt.Errorf("mac symbol bars body too short: %d", len(body))
	}

	bars := &MACSymbolBars{
		Market: binary.LittleEndian.Uint16(body[0:2]),
		Code:   trimNulCodeGBK(body[2:14]),
		Period: body[24],
		Count:  binary.LittleEndian.Uint16(body[27:29]),
		Start:  binary.LittleEndian.Uint32(body[29:33]),
	}

	formatTDXTime := bars.Period < 4 || bars.Period == 7 || bars.Period == 8
	pos := 33
	var preClose float64
	for i := uint16(0); i < bars.Count; i++ {
		if pos+36 > len(body) {
			break
		}
		ymd := binary.LittleEndian.Uint32(body[pos : pos+4])
		seconds := binary.LittleEndian.Uint32(body[pos+4 : pos+8])
		bar := MACSymbolBar{
			DateTime:    macCombineDateTime(ymd, seconds, formatTDXTime),
			Open:        float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+8 : pos+12]))),
			High:        float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+12 : pos+16]))),
			Low:         float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+16 : pos+20]))),
			Close:       float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+20 : pos+24]))),
			Amount:      float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+24 : pos+28]))),
			Vol:         float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+28 : pos+32]))),
			FloatShares: float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+32 : pos+36]))),
		}
		bar.PreClose = preClose
		preClose = bar.Close
		bar.RisePrice = macRisePrice(bar)
		bar.RiseRate = macRiseRate(bar)
		pos += 36

		// 第一根 K 线仅作 preClose 种子，跳过不入结果。
		if i == 0 {
			continue
		}
		bars.List = append(bars.List, bar)
	}
	bars.Count = uint16(len(bars.List))

	// 摘要尾部 (与 MACQuotes 共用 120 字节布局)。
	if pos+120 <= len(body) {
		var q MACQuote
		fillMACSummary120(&q, body[pos:pos+120])
		bars.Name = q.Name
		bars.Decimal = q.Decimal
		bars.Category = q.Category
		bars.VolUnit = q.VolUnit
		bars.DateTime = q.DateTime
		bars.PreClose = q.PreClose
		bars.Open = q.Open
		bars.High = q.High
		bars.Low = q.Low
		bars.Close = q.Close
		bars.Vol = q.Vol
		bars.Amount = q.Amount
		bars.Turnover = q.Turnover
		bars.Avg = q.Avg
		bars.Industry = q.Industry
		bars.IndustryCode = q.IndustryCode
	}
	return bars, nil
}

// macCombineDateTime 将打包日期 (YYYYMMDD) + 当日秒数转换为 time.Time。
// 对于分钟级周期 (formatTDXTime)，若小时 <=5 视为跨日，加 24 小时修正。
func macCombineDateTime(ymd uint32, seconds uint32, formatTDXTime bool) time.Time {
	year := int(ymd / 10000)
	month := int((ymd % 10000) / 100)
	day := int(ymd % 100)
	hours := int(seconds / 3600)
	minutes := int((seconds % 3600) / 60)
	ts := time.Date(year, time.Month(month), day, hours, minutes, 0, 0, time.Local)
	if formatTDXTime && ts.Hour() <= 5 {
		return ts.Add(24 * time.Hour)
	}
	return ts
}

// macRisePrice 计算涨跌价：有昨收则 close-preclose，否则 close-open。
func macRisePrice(b MACSymbolBar) float64 {
	if b.PreClose == 0 {
		return b.Close - b.Open
	}
	return b.Close - b.PreClose
}

// macRiseRate 计算涨跌幅 (百分比)。
func macRiseRate(b MACSymbolBar) float64 {
	if b.PreClose == 0 {
		if b.Open == 0 {
			return 0
		}
		return (b.Close - b.Open) / b.Open * 100
	}
	return (b.Close - b.PreClose) / b.PreClose * 100
}
