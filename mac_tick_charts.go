package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// MACTickChartItem 表示 0x123E MAC 多日分时的单个采样点。
type MACTickChartItem struct {
	Time  string  // 采样时刻 "HH:MM"
	Price float64 // 价格
	Avg   float64 // 均价
	Vol   uint16  // 成交量
}

// MACTickChartDay 表示单日的分时数据。
type MACTickChartDay struct {
	Date     string            // 日期 "2006-01-02"
	PreClose float64           // 当日昨收
	Ticks    []MACTickChartItem // 分时采样点
}

// MACTickCharts 表示 0x123E MAC 多日分时响应（含多日分时 + 摘要尾部）。
type MACTickCharts struct {
	Market       uint16            // 市场代码
	Code         string            // 股票代码
	Name         string            // 股票名称
	Count        uint16            // 天数
	Total        uint16            // 分时点总数
	Charts       []MACTickChartDay // 多日分时
	PreClose     float64           // 摘要昨收
	Open         float64           // 摘要开盘
	High         float64           // 摘要最高
	Low          float64           // 摘要最低
	Close        float64           // 摘要最新
	Vol          uint32            // 摘要成交量
	Amount       float64           // 摘要成交额
	Turnover     float64           // 摘要换手率
	Avg          float64           // 摘要均价
	DateTime     time.Time         // 摘要服务器时间
	Decimal      uint8             // 小数位
	Category     uint16            // 证券类别
	VolUnit      float64           // 成交量单位
	Industry     uint32            // 行业代码
	IndustryCode string            // 行业板块代码
}

// RequestMACTickChartsFrame 构建 0x123E MAC 多日分时请求帧。
//
// 请求体布局 (38 字节):
//
//	[0:2]   market    uint16 市场代码
//	[2:24]  code      [22]byte 股票代码
//	[24:28] queryDate uint32 起始日期 YYYYMMDD
//	[28:30] days      uint16 天数 (1-5)
//	[30:32] one       uint16 固定为 1
//	[32:38] 保留字段全 0
func RequestMACTickChartsFrame(msgID uint32, control byte, market uint16, code22 []byte, queryDate uint32, days uint16) ([]byte, error) {
	body := make([]byte, 38)
	binary.LittleEndian.PutUint16(body[0:2], market)
	copy(body[2:24], code22)
	binary.LittleEndian.PutUint32(body[24:28], queryDate)
	binary.LittleEndian.PutUint16(body[28:30], days)
	binary.LittleEndian.PutUint16(body[30:32], 1) // One 固定为 1
	return BuildSPFrame(0x01, DirectFrameTypeMACTickCharts, body), nil
}

// GetMACTickCharts 获取多日分时数据。
//
// 参数:
//   - code: 股票代码，支持 "600000"/"sh600000" 等格式
//   - date: 起始日期 "20060102" 或 "2006-01-02"，空表示当天
//   - days: 天数，1-5
//
// 需通过 DialSP/DialSPBest 建立的连接调用。
//
// 示例:
//
//	chart, err := client.GetMACTickCharts("sh600000", "", 5)
func (c *MainClient) GetMACTickCharts(code string, date string, days uint16) (*MACTickCharts, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}
	if days == 0 {
		days = 5
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

	packet, err := RequestMACTickChartsFrame(0x000A0401, 0x01, market, code22, queryDate, days)
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeMACTickCharts, 8*time.Second)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty mac tick charts response")
	}

	return DecodeMACTickCharts(response.Body.Decoded)
}

// DecodeMACTickCharts 解析 0x123E MAC 多日分时响应体。
//
// 响应布局:
//
//	[0:2]   market    uint16
//	[2:24]  code      GBK 22 字节
//	[24:44] dates[5]  5 × uint32 每日 YYYYMMDD
//	[44:64] preCloses[5] 5 × float32 每日昨收
//	[64:66] count     uint16 天数
//	[66]    sendLast  uint8
//	[67:69] pageSize  uint16
//	[69:71] total     uint16 分时点总数
//	随后 total 个分时点，每个 14 字节:
//	  [0:2]   minutes uint16 → "HH:MM"
//	  [2:6]   price   float32
//	  [6:10]  avg     float32
//	  [10:12] vol     uint16
//	  [12:14] unknown uint16
//	分时点之后是 120 字节摘要尾部 (与 MACQuotes 一致)。
//
// 跨天切分：当后一个点的 minute <= 前一个点时，表示进入新的一天。
func DecodeMACTickCharts(body []byte) (*MACTickCharts, error) {
	if len(body) < 71 {
		return nil, fmt.Errorf("mac tick charts body too short: %d", len(body))
	}

	tc := &MACTickCharts{
		Market: binary.LittleEndian.Uint16(body[0:2]),
		Code:   trimNulCodeGBK(body[2:24]),
	}

	var dates [5]uint32
	var preCloses [5]float32
	for i := range dates {
		dates[i] = binary.LittleEndian.Uint32(body[24+i*4 : 28+i*4])
		preCloses[i] = math.Float32frombits(binary.LittleEndian.Uint32(body[44+i*4 : 48+i*4]))
	}
	tc.Count = binary.LittleEndian.Uint16(body[64:66])
	tc.Total = binary.LittleEndian.Uint16(body[69:71])

	pos := 71
	type tickWithMinute struct {
		minute uint16
		item   MACTickChartItem
	}
	ticks := make([]tickWithMinute, 0, tc.Total)
	for i := uint16(0); i < tc.Total; i++ {
		if pos+14 > len(body) {
			break
		}
		minutes := binary.LittleEndian.Uint16(body[pos : pos+2])
		ticks = append(ticks, tickWithMinute{
			minute: minutes,
			item: MACTickChartItem{
				Time:  fmt.Sprintf("%02d:%02d", (minutes/60)%24, minutes%60),
				Price: float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+2 : pos+6]))),
				Avg:   float64(math.Float32frombits(binary.LittleEndian.Uint32(body[pos+6 : pos+10]))),
				Vol:   binary.LittleEndian.Uint16(body[pos+10 : pos+12]),
			},
		})
		pos += 14
	}

	if tc.Count > 0 {
		currentDay := 0
		day := newMACTickChartDay(dates, preCloses, currentDay)
		for i, tick := range ticks {
			day.Ticks = append(day.Ticks, tick.item)
			if currentDay >= int(tc.Count)-1 {
				continue
			}
			if i+1 < len(ticks) && ticks[i+1].minute <= tick.minute {
				tc.Charts = append(tc.Charts, day)
				currentDay++
				day = newMACTickChartDay(dates, preCloses, currentDay)
			}
		}
		tc.Charts = append(tc.Charts, day)
		for len(tc.Charts) < int(tc.Count) {
			tc.Charts = append(tc.Charts, newMACTickChartDay(dates, preCloses, len(tc.Charts)))
		}
	}

	if pos+120 <= len(body) {
		var q MACQuote
		fillMACSummary120(&q, body[pos:pos+120])
		tc.Name = q.Name
		tc.Decimal = q.Decimal
		tc.Category = q.Category
		tc.VolUnit = q.VolUnit
		tc.DateTime = q.DateTime
		tc.PreClose = q.PreClose
		tc.Open = q.Open
		tc.High = q.High
		tc.Low = q.Low
		tc.Close = q.Close
		tc.Vol = q.Vol
		tc.Amount = q.Amount
		tc.Turnover = q.Turnover
		tc.Avg = q.Avg
		tc.Industry = q.Industry
		tc.IndustryCode = q.IndustryCode
	}
	return tc, nil
}

// newMACTickChartDay 根据日期/昨收数组构造指定日的分时容器。
func newMACTickChartDay(dates [5]uint32, preCloses [5]float32, dayIndex int) MACTickChartDay {
	day := MACTickChartDay{}
	if dayIndex < len(dates) && dates[dayIndex] != 0 {
		t := macParseDate(dates[dayIndex])
		day.Date = t.Format("2006-01-02")
		day.PreClose = float64(preCloses[dayIndex])
	}
	return day
}
