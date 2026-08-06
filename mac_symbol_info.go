package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// MACSymbolInfo 表示 0x122A MAC 股票摘要响应。
//
// 该命令返回单只股票的 F10 摘要数据：基本信息 + 实时行情 + 内外盘 + 换手/均价等。
// 相比 GetFinance（财务数据），本命令侧重即时行情快照。
type MACSymbolInfo struct {
	Market        uint16    // 市场代码 (0=深 1=沪 2=北)
	Code          string    // 股票代码
	Name          string    // 股票名称
	DateTime      time.Time // 服务器时间
	Activity      uint32    // 活跃度
	PreClose      float64   // 昨收价
	Open          float64   // 开盘价
	High          float64   // 最高价
	Low           float64   // 最低价
	Close         float64   // 最新价/收盘价
	Momentum      float64   // 涨跌 (最新价 - 昨收)
	Vol           uint32    // 成交量 (股)
	Amount        float64   // 成交额 (元)
	InsideVolume  uint32    // 内盘
	OutsideVolume uint32    // 外盘
	Decimal       uint16    // 小数位数
	Turnover      float64   // 换手率
	Avg           float64   // 均价
}

// RequestMACSymbolInfoFrame 构建 0x122A MAC 股票摘要请求帧。
//
// 请求体布局 (40 字节):
//
//	[0:2]   market   uint16 市场代码
//	[2:24]  code     [22]byte 股票代码
//	[24:28] one      uint32 固定为 1
//	[28:40] 保留字段全 0
func RequestMACSymbolInfoFrame(msgID uint32, control byte, market uint16, code22 []byte) ([]byte, error) {
	body := make([]byte, 40)
	binary.LittleEndian.PutUint16(body[0:2], market)
	copy(body[2:24], code22)
	binary.LittleEndian.PutUint32(body[24:28], 1) // One 字段固定为 1
	return BuildSPFrame(0x01, DirectFrameTypeMACSymbolInfo, body), nil
}

// GetMACSymbolInfo 获取单只股票的 MAC 摘要信息。
// code 支持 "600000"/"sh600000" 等格式。需通过 DialSP/DialSPBest 建立的连接调用。
//
// 示例:
//
//	info, err := client.GetMACSymbolInfo("sh600000")
func (c *MainClient) GetMACSymbolInfo(code string) (*MACSymbolInfo, error) {
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

	packet, err := RequestMACSymbolInfoFrame(0x000A0401, 0x01, market, code22)
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeMACSymbolInfo, 8*time.Second)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty mac symbol info response")
	}

	return DecodeMACSymbolInfo(response.Body.Decoded)
}

// DecodeMACSymbolInfo 解析 0x122A MAC 股票摘要响应体。
//
// 响应布局 (至少 194 字节):
//
//	[0:8]    保留头部 (未使用)
//	[8:10]   market        uint16
//	[10:32]  code          GBK 22 字节
//	[32:76]  name          GBK 44 字节
//	[96:100] date          uint32 YYYYMMDD
//	[100:104] time         uint32 HHMMSS
//	[104:108] activity     uint32
//	[108:112] preClose     float32
//	[112:116] open         float32
//	[116:120] high         float32
//	[120:124] low          float32
//	[124:128] close        float32
//	[128:132] momentum     float32
//	[132:136] vol          uint32
//	[136:140] amount       float32
//	[140:144] insideVolume uint32
//	[144:148] outsideVolume uint32
//	[148:150] decimal      uint16
//	[186:190] turnover     float32
//	[190:194] avg          float32
func DecodeMACSymbolInfo(body []byte) (*MACSymbolInfo, error) {
	if len(body) < 194 {
		return nil, fmt.Errorf("mac symbol info body too short: %d", len(body))
	}

	info := &MACSymbolInfo{
		Market:        binary.LittleEndian.Uint16(body[8:10]),
		Code:          trimNulCodeGBK(body[10:32]),
		Name:          trimNulCodeGBK(body[32:76]),
		Activity:      binary.LittleEndian.Uint32(body[104:108]),
		PreClose:      float64(math.Float32frombits(binary.LittleEndian.Uint32(body[108:112]))),
		Open:          float64(math.Float32frombits(binary.LittleEndian.Uint32(body[112:116]))),
		High:          float64(math.Float32frombits(binary.LittleEndian.Uint32(body[116:120]))),
		Low:           float64(math.Float32frombits(binary.LittleEndian.Uint32(body[120:124]))),
		Close:         float64(math.Float32frombits(binary.LittleEndian.Uint32(body[124:128]))),
		Momentum:      float64(math.Float32frombits(binary.LittleEndian.Uint32(body[128:132]))),
		Vol:           binary.LittleEndian.Uint32(body[132:136]),
		Amount:        float64(math.Float32frombits(binary.LittleEndian.Uint32(body[136:140]))),
		InsideVolume:  binary.LittleEndian.Uint32(body[140:144]),
		OutsideVolume: binary.LittleEndian.Uint32(body[144:148]),
		Decimal:       binary.LittleEndian.Uint16(body[148:150]),
		Turnover:      float64(math.Float32frombits(binary.LittleEndian.Uint32(body[186:190]))),
		Avg:           float64(math.Float32frombits(binary.LittleEndian.Uint32(body[190:194]))),
	}
	info.DateTime = macParseDateTime(
		binary.LittleEndian.Uint32(body[96:100]),
		binary.LittleEndian.Uint32(body[100:104]),
	)
	return info, nil
}
