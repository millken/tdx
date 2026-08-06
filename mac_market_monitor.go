package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"
)

// MACMarketMonitorItem 表示 0x1237 MAC 市场监控的单条异动记录。
//
// 市场监控榜展示盘中异动个股（涨停封板、加速拉升、大单托盘等）及其触发时刻与数值。
type MACMarketMonitorItem struct {
	Index       uint16  // 序号
	Market      uint16  // 市场代码 (0=深 1=沪 2=北)
	Code        string  // 股票代码
	Name        string  // 股票名称 (响应末尾逗号分隔的名称列表)
	Time        string  // 触发时刻 "HH:MM:SS"
	UnusualType uint8   // 异动类型原始码
	Desc        string  // 异动描述 (如 "封涨停板")
	Value       string  // 异动数值 (如 "12.34/56.78" 或 "9.87%")
	V1          uint8   // 原始字段 v1
	V2          float64 // 原始字段 v2
	V3          float64 // 原始字段 v3
	V4          float64 // 原始字段 v4
}

// RequestMACMarketMonitorFrame 构建 0x1237 MAC 市场监控请求帧。
//
// 请求体布局 (22 字节):
//
//	[0:2]   market    uint16 市场代码
//	[2:4]   start     uint16 起始位置
//	[4:6]   reserved1 uint16
//	[6:8]   count     uint16 请求数量
//	[8:10]  reserved2 uint16
//	[10:12] mode      uint16 模式 (默认 1)
//	[12:22] limits    [5]uint16 各档限制 (默认 200,30,40,50,200)
func RequestMACMarketMonitorFrame(msgID uint32, control byte, market uint16, start uint16, count uint16, mode uint16, limits [5]uint16) ([]byte, error) {
	body := make([]byte, 22)
	binary.LittleEndian.PutUint16(body[0:2], market)
	binary.LittleEndian.PutUint16(body[2:4], start)
	binary.LittleEndian.PutUint16(body[6:8], count)
	binary.LittleEndian.PutUint16(body[10:12], mode)
	for i, v := range limits {
		binary.LittleEndian.PutUint16(body[12+i*2:14+i*2], v)
	}
	return BuildSPFrame(0x01, DirectFrameTypeMACMarketMonitor, body), nil
}

// GetMACMarketMonitor 获取市场监控异动榜。
//
// 参数:
//   - market: 市场代码，0=深 1=沪 2=北；传其他值（如 9）可表示全市场
//   - start: 起始位置
//   - count: 请求数量
//
// 需通过 DialSP/DialSPBest 建立的连接调用。
//
// 示例:
//
//	items, err := client.GetMACMarketMonitor(tdx.MarketShanghai, 0, 200)
func (c *MainClient) GetMACMarketMonitor(market uint16, start uint16, count uint16) ([]MACMarketMonitorItem, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}
	if count == 0 {
		count = 600
	}

	packet, err := RequestMACMarketMonitorFrame(0x000A0401, 0x01, market, start, count, 1, [5]uint16{200, 30, 40, 50, 200})
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeMACMarketMonitor, 8*time.Second)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty mac market monitor response")
	}

	return DecodeMACMarketMonitor(response.Body.Decoded)
}

// DecodeMACMarketMonitor 解析 0x1237 MAC 市场监控响应体。
//
// 响应布局:
//
//	[0:2] count uint16 返回条数
//	随后 count 条记录，每条 32 字节:
//	  [0:2]   market      uint16
//	  [2:8]   code        GBK 6 字节
//	  [9]     unusualType uint8 异动类型
//	  [11:13] index       uint16
//	  [15:28] payload     13 字节 (v1/v2/v3/v4，喂给 unpackMACUnusualByType)
//	  [29]    hour        uint8
//	  [30:32] minSec      uint16 (分钟=raw/100, 秒=raw%100)
//	二进制部分之后是逗号分隔的 GBK 名称列表，按序填入各条记录的 Name。
func DecodeMACMarketMonitor(body []byte) ([]MACMarketMonitorItem, error) {
	if len(body) < 2 {
		return nil, fmt.Errorf("mac market monitor body too short: %d", len(body))
	}
	count := int(binary.LittleEndian.Uint16(body[0:2]))

	items := make([]MACMarketMonitorItem, 0, count)
	for i := 0; i < count; i++ {
		base := 2 + i*32
		if base+32 > len(body) {
			break
		}
		payload := body[base+15 : base+28]
		v1 := payload[0]
		v2 := float64(math.Float32frombits(binary.LittleEndian.Uint32(payload[1:5])))
		v3 := float64(math.Float32frombits(binary.LittleEndian.Uint32(payload[5:9])))
		v4 := float64(math.Float32frombits(binary.LittleEndian.Uint32(payload[9:13])))
		unusualType := body[base+9]
		desc, value := unpackMACUnusualByType(unusualType, payload)
		minSec := binary.LittleEndian.Uint16(body[base+30 : base+32])

		items = append(items, MACMarketMonitorItem{
			Index:       binary.LittleEndian.Uint16(body[base+11 : base+13]),
			Market:      binary.LittleEndian.Uint16(body[base : base+2]),
			Code:        trimNulCodeGBK(body[base+2 : base+8]),
			Time:        fmt.Sprintf("%02d:%02d:%02d", int(body[base+29]), int(minSec/100), int(minSec%100)),
			UnusualType: unusualType,
			Desc:        desc,
			Value:       value,
			V1:          v1,
			V2:          v2,
			V3:          v3,
			V4:          v4,
		})
	}

	// 二进制部分之后的逗号分隔名称列表。
	binaryLength := 2 + len(items)*32
	if binaryLength < len(body) {
		names := strings.Trim(trimNulCodeGBK(body[binaryLength:]), ",")
		if names != "" {
			parts := strings.Split(names, ",")
			for i := range items {
				if i < len(parts) {
					items[i].Name = parts[i]
				}
			}
		}
	}
	return items, nil
}

// unpackMACUnusualByType 根据异动类型码解析描述与数值。
// data 为 13 字节 payload (v1:1 + v2:4 + v3:4 + v4:4)。
// 数据来自 gotdx/proto/get_unusual.go。
func unpackMACUnusualByType(eventType byte, data []byte) (string, string) {
	if len(data) < 13 {
		return "", ""
	}

	v1 := data[0]
	v2 := math.Float32frombits(binary.LittleEndian.Uint32(data[1:5]))
	v3 := math.Float32frombits(binary.LittleEndian.Uint32(data[5:9]))
	v4 := math.Float32frombits(binary.LittleEndian.Uint32(data[9:13]))

	switch eventType {
	case 0x03: // 主力买卖
		if v1 == 0x00 {
			return "主力买入", fmt.Sprintf("%.2f/%.2f", v2, v3)
		}
		return "主力卖出", fmt.Sprintf("%.2f/%.2f", v2, v3)
	case 0x04: // 加速拉升
		return "加速拉升", fmt.Sprintf("%.2f%%", v2*100)
	case 0x05: // 加速下跌
		return "加速下跌", ""
	case 0x06: // 低位反弹
		return "低位反弹", fmt.Sprintf("%.2f%%", v2*100)
	case 0x07: // 高位回落
		return "高位回落", fmt.Sprintf("%.2f%%", v2*100)
	case 0x08: // 撑杆跳高
		return "撑杆跳高", fmt.Sprintf("%.2f%%", v2*100)
	case 0x09: // 平台跳水
		return "平台跳水", fmt.Sprintf("%.2f%%", v2*100)
	case 0x0a: // 单笔冲涨/跌
		if v2 < 0 {
			return "单笔冲跌", fmt.Sprintf("%.2f%%", v2*100)
		}
		return "单笔冲涨", fmt.Sprintf("%.2f%%", v2*100)
	case 0x0b: // 区间放量
		if v3 == 0 {
			return "区间放量平", fmt.Sprintf("%.1f倍", v2)
		}
		if v3 < 0 {
			return "区间放量跌", fmt.Sprintf("%.1f倍%.2f%%", v2, v3*100)
		}
		return "区间放量涨", fmt.Sprintf("%.1f倍%.2f%%", v2, v3*100)
	case 0x0c: // 区间缩量
		return "区间缩量", ""
	case 0x10: // 大单托盘
		return "大单托盘", fmt.Sprintf("%.2f/%.2f", v4, v3)
	case 0x11: // 大单压盘
		return "大单压盘", fmt.Sprintf("%.2f/%.2f", v2, v3)
	case 0x12: // 大单锁盘
		return "大单锁盘", ""
	case 0x13: // 竞价试买
		return "竞价试买", fmt.Sprintf("%.2f/%.2f", v2, v3)
	case 0x14: // 涨跌停相关
		subType := data[1]
		uv2 := math.Float32frombits(binary.LittleEndian.Uint32(data[2:6]))
		uv3 := math.Float32frombits(binary.LittleEndian.Uint32(data[6:10]))
		direction := "涨"
		if v1 != 0x00 {
			direction = "跌"
		}
		desc := ""
		switch subType {
		case 0x01:
			desc = "逼近" + direction + "停"
		case 0x02:
			desc = "封" + direction + "停板"
		case 0x04:
			desc = "封" + direction + "大减"
		case 0x05:
			desc = "打开" + direction + "停"
		}
		return desc, fmt.Sprintf("%.2f/%.2f", uv2, uv3)
	case 0x15: // 尾盘异动
		desc := "尾盘打压"
		switch v1 {
		case 0x00:
			desc = "尾盘异动"
		case 0x01:
			desc = "尾盘对倒"
		case 0x02:
			desc = "尾盘拉升"
		}
		return desc, fmt.Sprintf("%.2f%%/%.2f", v2*100, v3)
	case 0x16: // 盘中强弱
		if v2 < 0 {
			return "盘中弱势", fmt.Sprintf("%.2f%%", v2*100)
		}
		return "盘中强势", fmt.Sprintf("%.2f%%", v2*100)
	case 0x1d: // 急速拉升
		return "急速拉升", fmt.Sprintf("%.2f%%", v2*100)
	case 0x1e: // 急速下跌
		return "急速下跌", fmt.Sprintf("%.2f%%", v2*100)
	default:
		return "", ""
	}
}
