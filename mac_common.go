package tdx

import (
	"fmt"
	"time"
)

// 本文件提供 MAC (mac_quotation, 0x12xx 系列命令) 协议共用的辅助函数。
//
// MAC 协议的股票代码固定占 22 字节（不足补 0），市场代码与主站行情一致：
// MarketShenzhen=0, MarketShanghai=1, MarketBeijing=2。所有 MAC 命令经 SP
// 登录后走 head=0x01 帧（见 BuildSPFrame），价格字段为 IEEE-754 float32
// (little-endian)，无 varint 或除法缩放。

// macCode22 将 6 位股票代码拷贝进 22 字节缓冲（剩余字节保持 0）。
func macCode22(code string) ([]byte, error) {
	if len(code) > 22 {
		return nil, fmt.Errorf("tdx: mac code too long: %q", code)
	}
	buf := make([]byte, 22)
	copy(buf, code)
	return buf, nil
}

// macResolveCode 把用户传入的 code（支持 "600000"/"sh600000" 等格式）解析为
// (归一化代码, 市场代码)。与 GetTransaction/GetKline 等使用同一套解析逻辑。
func macResolveCode(code string) (string, uint16, error) {
	normalizedCode, market, hasPrefixedMarket, err := normalizeKlineCode(code)
	if err != nil {
		return "", 0, err
	}
	if !hasPrefixedMarket {
		market, err = inferKlineMarket(normalizedCode)
		if err != nil {
			return "", 0, err
		}
	}
	return normalizedCode, market, nil
}

// macParseDate 将打包的十进制整数日期 YYYYMMDD 解析为 time.Time。
func macParseDate(dateRaw uint32) time.Time {
	year := int(dateRaw / 10000)
	month := int((dateRaw % 10000) / 100)
	day := int(dateRaw % 100)
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
}

// macParseDateTime 将打包的十进制日期时间 (YYYYMMDD, HHMMSS) 解析为 time.Time。
func macParseDateTime(dateRaw uint32, timeRaw uint32) time.Time {
	year := int(dateRaw / 10000)
	month := int((dateRaw % 10000) / 100)
	day := int(dateRaw % 100)
	hour := int(timeRaw / 10000)
	minute := int((timeRaw % 10000) / 100)
	second := int(timeRaw % 100)
	return time.Date(year, time.Month(month), day, hour, minute, second, 0, time.Local)
}

// macIndustryBoardSymbolMap 是通达信行业代码到行业板块代码 (881xxx) 的映射表。
// 数据来自 gotdx/proto/field_alignment_helpers.go。
var macIndustryBoardSymbolMap = map[string]string{
	"1001": "881002",
	"1102": "881008",
	"1103": "881011",
	"1201": "881016",
	"1202": "881019",
	"1203": "881026",
	"1204": "881034",
	"1205": "881044",
	"1206": "881051",
	"1207": "881055",
	"1208": "881104",
	"1301": "881062",
	"1302": "881065",
	"1303": "881069",
	"1401": "881071",
	"1402": "881075",
	"1403": "881078",
	"1404": "881082",
	"1405": "881087",
	"1501": "881091",
	"1502": "881094",
	"1503": "881097",
	"2001": "881106",
	"2002": "881111",
	"2003": "881115",
	"2004": "881116",
	"2005": "881119",
	"2006": "881123",
	"2007": "881127",
	"2102": "881130",
	"2103": "881136",
	"2104": "881139",
	"2105": "881140",
	"2106": "881144",
	"2201": "881151",
	"2202": "881157",
	"2203": "881162",
	"2301": "881167",
	"2302": "881171",
	"2303": "881177",
	"2304": "881180",
	"2401": "881184",
	"2402": "881187",
	"2403": "881190",
	"2404": "881194",
	"2406": "881198",
	"2501": "881200",
	"2503": "881204",
	"2504": "881205",
	"2505": "881206",
	"2506": "881207",
	"2601": "881212",
	"2602": "881215",
	"2603": "881218",
	"2604": "881224",
	"2605": "881227",
	"2701": "881231",
	"2702": "881234",
	"2703": "881241",
	"2704": "881247",
	"2705": "881252",
	"2706": "881256",
	"2707": "881257",
	"3001": "881261",
	"3002": "881262",
	"3003": "881268",
	"3004": "881275",
	"3005": "881282",
	"3006": "881285",
	"3101": "881287",
	"3102": "881288",
	"3103": "881289",
	"3104": "881290",
	"3105": "881291",
	"3201": "881293",
	"3202": "881294",
	"3203": "881303",
	"3204": "881310",
	"3205": "881313",
	"4001": "881319",
	"4002": "881326",
	"4003": "881329",
	"4004": "881333",
	"4005": "881336",
	"4101": "881338",
	"4102": "881344",
	"4103": "881347",
	"4201": "881352",
	"4202": "881355",
	"4203": "881359",
	"4204": "881364",
	"4301": "881369",
	"4302": "881370",
	"4303": "881373",
	"4304": "881376",
	"4306": "881380",
	"4307": "881384",
	"5001": "881386",
	"5002": "881389",
	"5101": "881394",
	"5102": "881395",
	"5103": "881396",
	"5201": "881406",
	"5202": "881407",
	"5203": "881410",
	"5204": "881415",
	"5205": "881416",
	"5301": "881418",
	"5302": "881422",
	"6001": "881427",
	"6002": "881428",
	"6003": "881429",
	"6005": "881432",
	"6006": "881436",
	"6101": "881442",
	"6102": "881446",
	"6103": "881449",
	"6104": "881452",
	"6201": "881459",
	"6202": "881467",
	"6203": "881468",
	"6301": "881470",
	"6302": "881471",
	"6303": "881476",
	"9901": "881478",
}

// macIndustryBoardSymbol 将行业代码 (uint32) 转换为行业板块代码 (881xxx)。
// 无法识别时返回空字符串。
func macIndustryBoardSymbol(industry uint32) string {
	if industry == 0 {
		return ""
	}
	return macIndustryBoardSymbolMap[fmt.Sprintf("%04d", industry%10000)]
}
