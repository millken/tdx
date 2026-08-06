package tdx

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// MACCapitalFlow 表示 0x1218 MAC 资金流向响应。
//
// 该命令是 MAC 协议中的特例：请求头 head=0x02 (其余 MAC 命令为 0x01)，
// 响应体为「27 字节二进制头 + JSON 数组」。JSON 含两组数据：
// rows[0] 为当日资金 (主力流入/流出/散户流入/流出)，rows[1] 为 5 日资金
// (主力买/卖/超大单/大单/中单/小单净额)。
type MACCapitalFlow struct {
	Market           uint16   // 市场代码
	QueryInfo        string   // 查询信息 (GBK)
	Ext              string   // 扩展信息 (GBK)
	Today            []float64 // 当日原始资金数组
	FiveDays         []float64 // 5 日原始资金数组
	TodayMainIn      float64  // 当日主力流入 (元)
	TodayMainOut     float64  // 当日主力流出 (元)
	TodayRetailIn    float64  // 当日散户流入 (元)
	TodayRetailOut   float64  // 当日散户流出 (元)
	TodayMainNetIn   float64  // 当日主力净流入 (流入 - 流出)
	TodayRetailNetIn float64  // 当日散户净流入
	FiveDayMainBuy   float64  // 5 日主力买入
	FiveDayMainSell  float64  // 5 日主力卖出
	FiveDaySuperNet  float64  // 5 日超大单净额
	FiveDayLargeNet  float64  // 5 日大单净额
	FiveDayMediumNet float64  // 5 日中单净额
	FiveDaySmallNet  float64  // 5 日小单净额
	FiveDayMainNetIn float64  // 5 日主力净流入 (买入 - 卖出)
}

// RequestMACCapitalFlowFrame 构建 0x1218 MAC 资金流向请求帧 (head=0x02)。
//
// 请求体布局 (47 字节):
//
//	[0:2]   market   uint16 市场代码
//	[2:10]  symbol   [8]byte 股票代码
//	[10:26] 保留字段全 0
//	[26:47] query    [21]byte 固定为 "Stock_ZJLX"
func RequestMACCapitalFlowFrame(msgID uint32, control byte, market uint16, code string) ([]byte, error) {
	body := make([]byte, 47)
	binary.LittleEndian.PutUint16(body[0:2], market)
	if len(code) > 8 {
		return nil, fmt.Errorf("tdx: capital flow symbol too long: %q", code)
	}
	copy(body[2:10], code)
	copy(body[26:47], "Stock_ZJLX")
	// head=0x02 是该命令区别于其他 MAC 命令的关键。
	return BuildSPFrame(0x02, DirectFrameTypeMACCapitalFlow, body), nil
}

// GetMACCapitalFlow 获取资金流向数据。
//
// 参数:
//   - code: 股票代码，支持 "600000"/"sh600000" 等格式
//
// 需通过 DialSP/DialSPBest 建立的连接调用。
//
// 示例:
//
//	flow, err := client.GetMACCapitalFlow("sh600000")
func (c *MainClient) GetMACCapitalFlow(code string) (*MACCapitalFlow, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	normalizedCode, market, err := macResolveCode(code)
	if err != nil {
		return nil, err
	}

	packet, err := RequestMACCapitalFlowFrame(0x000A0401, 0x01, market, normalizedCode)
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeMACCapitalFlow, 8*time.Second)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty mac capital flow response")
	}

	return DecodeMACCapitalFlow(response.Body.Decoded)
}

// DecodeMACCapitalFlow 解析 0x1218 MAC 资金流向响应体。
//
// 响应布局:
//
//	[0:2]   market    uint16
//	[2:14]  queryInfo GBK 12 字节
//	[19:27] ext       GBK 8 字节
//	[27:]   JSON      [][]any，rows[0]=当日资金，rows[1]=5日资金
func DecodeMACCapitalFlow(body []byte) (*MACCapitalFlow, error) {
	if len(body) < 27 {
		return nil, fmt.Errorf("mac capital flow body too short: %d", len(body))
	}

	cf := &MACCapitalFlow{
		Market:    binary.LittleEndian.Uint16(body[0:2]),
		QueryInfo: trimNulCodeGBK(body[2:14]),
		Ext:       trimNulCodeGBK(body[19:27]),
	}

	var rows [][]interface{}
	if err := json.Unmarshal(body[27:], &rows); err != nil {
		return nil, fmt.Errorf("mac capital flow json: %w", err)
	}
	if len(rows) > 0 {
		cf.Today = anySliceToFloat64(rows[0])
	}
	if len(rows) > 1 {
		cf.FiveDays = anySliceToFloat64(rows[1])
	}

	if len(cf.Today) >= 4 {
		cf.TodayMainIn = cf.Today[0]
		cf.TodayMainOut = cf.Today[1]
		cf.TodayRetailIn = cf.Today[2]
		cf.TodayRetailOut = cf.Today[3]
		cf.TodayMainNetIn = cf.TodayMainIn - cf.TodayMainOut
		cf.TodayRetailNetIn = cf.TodayRetailIn - cf.TodayRetailOut
	}
	if len(cf.FiveDays) >= 6 {
		cf.FiveDayMainBuy = cf.FiveDays[0]
		cf.FiveDayMainSell = cf.FiveDays[1]
		cf.FiveDaySuperNet = cf.FiveDays[2]
		cf.FiveDayLargeNet = cf.FiveDays[3]
		cf.FiveDayMediumNet = cf.FiveDays[4]
		cf.FiveDaySmallNet = cf.FiveDays[5]
		cf.FiveDayMainNetIn = cf.FiveDayMainBuy - cf.FiveDayMainSell
	}
	return cf, nil
}

// anySliceToFloat64 将 []any 转换为 []float64，元素可为 float64/json.Number/string。
func anySliceToFloat64(values []interface{}) []float64 {
	out := make([]float64, 0, len(values))
	for _, v := range values {
		out = append(out, anyToFloat64(v))
	}
	return out
}

func anyToFloat64(v interface{}) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case json.Number:
		f, _ := value.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(value, 64)
		return f
	default:
		return 0
	}
}
