// Package tdx provides access to TDX cloud chart data from excalc servers.
//
// Cloud chart (板块云图) data is served by excalc.icfqs.com:7616 as static
// JavaScript files containing base64-encoded Protobuf messages.
//
// Endpoints:
//   - GET /site/statics/jsdata/dp_stat.js   → market index status (JSON)
//   - GET /site/statics/jsdata/real_hq.js   → real-time stock quotes (Protobuf)
//   - GET /site/statics/jsdata/codetable_N.js → stock lists (Protobuf), N=12..18
package tdx

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ExcalcHost is the primary cloud chart server.
const ExcalcHost = "excalc2.icfqs.com:7616"

// ExcalcBackupHost is the backup cloud chart server.
const ExcalcBackupHost = "excalc.icfqs.com:7616"

// Setcode identifies which exchange a stock belongs to.
type Setcode int

const (
	SetcodeSZ Setcode = 0 // Shenzhen Stock Exchange (深圳证券交易所)
	SetcodeSH Setcode = 1 // Shanghai Stock Exchange (上海证券交易所)
)

// MarketStat contains the cloud chart market status (from dp_stat.js).
type MarketStat struct {
	// SBTSize is the number of sub-board types.
	SBTSize int `json:"SBTSize"`
	// ListHead defines column names for the List entries.
	ListHead []string `json:"ListHead"`
	// List contains market index data rows.
	// Each row: [Code, Setcode, Name, CLOSE, NOW, BUYV1, SELLV1]
	List [][]json.RawMessage `json:"List"`
}

// MarketStatEntry is a parsed row from MarketStat.List.
type MarketStatEntry struct {
	Code    string
	Setcode Setcode
	Name    string
	Close   string
	Now     string
	BuyV1   int64
	SellV1  int64
}

// StockQuote contains real-time quote data for one stock (from real_hq.js).
type StockQuote struct {
	Code      string  // 股票代码
	TotalMcap float32 // 总市值（元）（proto JZsz id:3）
	YClose    float32 // 昨收价（元）（proto ZClose id:4）
	Now       float32 // 最新价（元）（proto Now id:5）
	DayChgPct float32 // 当前涨幅（小数，-0.0165=-1.65%）（proto dqzf id:6）
	Zangsu    float32 // 涨速（proto fZangsu id:7）
	Turnover  float32 // 换手率百分比（proto fHSL id:8）
	VolRatio  float32 // 量比（proto FlianB id:9）
	Chg3d     float32 // 3日涨幅（小数）（proto ZAFPre3 id:10）
	MainNetAmt float32 // 主力净额（元，正=净买入，负=净卖出）（proto fAmoSum id:11）
	FloatMcap float32 // 流通市值（元）（proto JLtsz id:12）
	Amount    float32 // 成交额（元）（proto Amount id:13）
}

// StockInfo contains stock metadata (from codetable_N.js).
type StockInfo struct {
	Index        int     // position index within the entry
	Setcode      Setcode // exchange
	Code         string  // 股票代码
	Name         string  // 股票名称
	IndustryCode string  // 行业代码
	IndustryName string  // 行业名称
}

var excalcUA = "Mozilla/5.0 (Windows NT 6.1; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.102 Safari/537.36 TdxW"

// ExcalcClient fetches cloud chart data from an excalc server.
type ExcalcClient struct {
	host   string
	client *http.Client
}

// NewExcalcClient creates a client targeting the given host (e.g. "excalc2.icfqs.com:7616").
func NewExcalcClient(host string) *ExcalcClient {
	return &ExcalcClient{
		host: host,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *ExcalcClient) get(path string) ([]byte, error) {
	url := "http://" + c.host + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", excalcUA)
	// Do NOT set Accept-Encoding manually; Go's transport handles gzip
	// transparently when it adds the header itself.

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("excalc: HTTP %d for %s", resp.StatusCode, path)
	}
	return io.ReadAll(resp.Body)
}

// GetMarketStat fetches the market index status from dp_stat.js.
func (c *ExcalcClient) GetMarketStat() (*MarketStat, error) {
	body, err := c.get("/site/statics/jsdata/dp_stat.js")
	if err != nil {
		return nil, err
	}
	// Format: window['G_DP_STAT']={...}
	s := string(body)
	idx := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if idx < 0 || end < idx {
		return nil, fmt.Errorf("excalc: unexpected dp_stat.js format")
	}
	var stat MarketStat
	if err := json.Unmarshal([]byte(s[idx:end+1]), &stat); err != nil {
		return nil, fmt.Errorf("excalc: parse dp_stat.js: %w", err)
	}
	return &stat, nil
}

// Entries returns the parsed list entries from a MarketStat.
func (ms *MarketStat) Entries() ([]MarketStatEntry, error) {
	out := make([]MarketStatEntry, 0, len(ms.List))
	for _, row := range ms.List {
		if len(row) < 7 {
			continue
		}
		e := MarketStatEntry{}
		if err := json.Unmarshal(row[0], &e.Code); err != nil {
			return nil, err
		}
		var sc int
		if err := json.Unmarshal(row[1], &sc); err != nil {
			return nil, err
		}
		e.Setcode = Setcode(sc)
		if err := json.Unmarshal(row[2], &e.Name); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(row[3], &e.Close); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(row[4], &e.Now); err != nil {
			return nil, err
		}
		json.Unmarshal(row[5], &e.BuyV1)  //nolint:errcheck
		json.Unmarshal(row[6], &e.SellV1) //nolint:errcheck
		out = append(out, e)
	}
	return out, nil
}

var reJSArray = regexp.MustCompile(`=\s*\[([^\]]*)\]`)

// extractBase64Array parses the array of base64 strings from a JS file like:
//
//	window.G_XXX = ["base64", "base64", ...]
func extractBase64Array(body []byte) ([][]byte, error) {
	m := reJSArray.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("excalc: JS array not found")
	}
	// Find all quoted base64 strings
	re := regexp.MustCompile(`"([A-Za-z0-9+/=]+)"`)
	matches := re.FindAllSubmatch(m[1], -1)
	out := make([][]byte, 0, len(matches))
	for _, match := range matches {
		data, err := base64.StdEncoding.DecodeString(string(match[1]))
		if err != nil {
			continue
		}
		out = append(out, data)
	}
	return out, nil
}

// GetRealHQ fetches real-time stock quotes from real_hq.js.
func (c *ExcalcClient) GetRealHQ() ([]StockQuote, error) {
	body, err := c.get("/site/statics/jsdata/real_hq.js")
	if err != nil {
		return nil, err
	}
	items, err := extractBase64Array(body)
	if err != nil {
		return nil, err
	}
	out := make([]StockQuote, 0, len(items)*40) // rough estimate: ~40 stocks per batch
	for _, data := range items {
		quotes, err := parseRealHQ(data)
		if err != nil {
			continue
		}
		out = append(out, quotes...)
	}
	return out, nil
}

// parseRealHQ decodes one base64 Protobuf batch from G_REAL_HQ.
// Each batch contains multiple repeated field-1 sub-messages.
//
// G_REAL_HQ Protobuf schema (verified by cross-referencing with TDX 7709 API,
// 2026-05-11 during market hours; partial fields remain unconfirmed):
//
//	message Batch {
//	  repeated StockQuote stocks = 1;
//	}
//	message StockQuote {
//	  string  code       = 2;  // 股票代码（6位）
//	  float   total_mcap = 3;  // 总市值（元）= 总股本 × NOW  ✓
//	  float   now        = 4;  // 最新价（元）  ✓
//	  float   day_close  = 5;  // 今日K线收盘价（盘中=实时价）  ✓
//	  float   day_chg    = 6;  // (day_close - now) / now  ✓
//	  float   field7     = 7;  // 疑似 N 日历史涨幅，尚未确认
//	  float   turnover   = 8;  // 换手率百分比（0.21 = 0.21%）  ✓
//	  float   vol_ratio  = 9;  // 量比（≥0，1.0=正常速度）  ✓
//	  float   change     = 10; // 涨跌幅（有符号小数，-0.0165 = -1.65%）  ✓
//	  float   field11    = 11; // 疑似资金净流向，尚未确认
//	  float   float_mcap = 12; // 流通市值（元）= 流通股本 × NOW  ✓
//	  float   amount     = 13; // 成交额（元）  ✓
//	}
func parseRealHQ(data []byte) ([]StockQuote, error) {
	var quotes []StockQuote
	pos := 0
	for pos < len(data) {
		tag, n := pbVarint(data, pos)
		if n <= 0 {
			break
		}
		pos += n
		field := tag >> 3
		wire := tag & 0x7
		if field == 1 && wire == 2 {
			// Length-delimited sub-message
			length, n := pbVarint(data, pos)
			if n <= 0 {
				break
			}
			pos += n
			if pos+int(length) > len(data) {
				break
			}
			sub := data[pos : pos+int(length)]
			pos += int(length)
			q, err := parseOneQuote(sub)
			if err == nil {
				quotes = append(quotes, q)
			}
		} else {
			// Skip unknown field
			if err := pbSkip(data, &pos, wire); err != nil {
				break
			}
		}
	}
	return quotes, nil
}

func parseOneQuote(data []byte) (StockQuote, error) {
	var q StockQuote
	pos := 0
	for pos < len(data) {
		tag, n := pbVarint(data, pos)
		if n <= 0 {
			break
		}
		pos += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 2 && wire == 2:
			s, n, err := pbString(data, pos)
			if err != nil {
				return q, err
			}
			pos += n
			q.Code = s
		case field == 3 && wire == 5:
			if pos+4 > len(data) {
				return q, fmt.Errorf("excalc: truncated f3")
			}
			q.TotalMcap = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
		case field == 4 && wire == 5:
			if pos+4 > len(data) {
				return q, fmt.Errorf("excalc: truncated f4")
			}
			q.YClose = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
		case field == 5 && wire == 5:
			if pos+4 > len(data) {
				return q, fmt.Errorf("excalc: truncated f5")
			}
			q.Now = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
		case field == 6 && wire == 5:
			if pos+4 > len(data) {
				return q, fmt.Errorf("excalc: truncated f6")
			}
			q.DayChgPct = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
		case field == 7 && wire == 5:
			if pos+4 > len(data) {
				return q, fmt.Errorf("excalc: truncated f7")
			}
			q.Zangsu = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
		case field == 8 && wire == 5:
			if pos+4 > len(data) {
				return q, fmt.Errorf("excalc: truncated f8")
			}
			q.Turnover = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
		case field == 9 && wire == 5:
			if pos+4 > len(data) {
				return q, fmt.Errorf("excalc: truncated f9")
			}
			q.VolRatio = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
		case field == 10 && wire == 5:
			if pos+4 > len(data) {
				return q, fmt.Errorf("excalc: truncated f10")
			}
			q.Chg3d = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
		case field == 11 && wire == 5:
			if pos+4 > len(data) {
				return q, fmt.Errorf("excalc: truncated f11")
			}
			q.MainNetAmt = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
		case field == 12 && wire == 5:
			if pos+4 > len(data) {
				return q, fmt.Errorf("excalc: truncated f12")
			}
			q.FloatMcap = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
		case field == 13 && wire == 5:
			if pos+4 > len(data) {
				return q, fmt.Errorf("excalc: truncated f13")
			}
			q.Amount = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
			pos += 4
		default:
			if err := pbSkip(data, &pos, wire); err != nil {
				return q, err
			}
		}
	}
	if q.Code == "" {
		return q, fmt.Errorf("excalc: empty code in quote")
	}
	return q, nil
}

// GetCodeTable fetches the stock list for a given setcode (exchange).
// N corresponds to exchange IDs; known values: 12..18 (SZ/SH boards).
// Use GetStockList for a friendlier API.
func (c *ExcalcClient) GetCodeTable(n int) ([]StockInfo, error) {
	path := fmt.Sprintf("/site/statics/jsdata/codetable_%d.js", n)
	body, err := c.get(path)
	if err != nil {
		return nil, err
	}
	items, err := extractBase64Array(body)
	if err != nil {
		return nil, err
	}
	var out []StockInfo
	for _, data := range items {
		stocks, err := parseCodeTable(data)
		if err != nil {
			continue
		}
		out = append(out, stocks...)
	}
	return out, nil
}

// knownCodeTableIDs lists the codetable_N.js IDs observed in TDX traffic.
var knownCodeTableIDs = []int{12, 13, 14, 15, 16, 17, 18}

// GetAllStocks fetches stock lists from all known codetable_N.js files.
func (c *ExcalcClient) GetAllStocks() ([]StockInfo, error) {
	var out []StockInfo
	for _, n := range knownCodeTableIDs {
		stocks, err := c.GetCodeTable(n)
		if err != nil {
			// Ignore missing tables
			continue
		}
		out = append(out, stocks...)
	}
	return out, nil
}

// parseCodeTable decodes one base64 Protobuf batch from G_CODE_TABLE_N.
//
// G_CODE_TABLE_N Protobuf schema (observed fields):
//
//	message Batch {
//	  repeated StockInfo items = 1;
//	}
//	message StockInfo {
//	  int32  index         = 1;  // position index (0 when omitted)
//	  int32  setcode       = 2;  // 0=SZ, 1=SH (0 when omitted)
//	  string code          = 3;  // 股票代码
//	  string name          = 4;  // 股票名称
//	  string industry_code = 5;  // 行业代码
//	  string industry_name = 6;  // 行业名称
//	}
func parseCodeTable(data []byte) ([]StockInfo, error) {
	var stocks []StockInfo
	pos := 0
	for pos < len(data) {
		tag, n := pbVarint(data, pos)
		if n <= 0 {
			break
		}
		pos += n
		field := tag >> 3
		wire := tag & 0x7
		if field == 1 && wire == 2 {
			length, n := pbVarint(data, pos)
			if n <= 0 {
				break
			}
			pos += n
			if pos+int(length) > len(data) {
				break
			}
			sub := data[pos : pos+int(length)]
			pos += int(length)
			s, err := parseOneStock(sub)
			if err == nil {
				stocks = append(stocks, s)
			}
		} else {
			if err := pbSkip(data, &pos, wire); err != nil {
				break
			}
		}
	}
	return stocks, nil
}

func parseOneStock(data []byte) (StockInfo, error) {
	var s StockInfo
	pos := 0
	for pos < len(data) {
		tag, n := pbVarint(data, pos)
		if n <= 0 {
			break
		}
		pos += n
		field := tag >> 3
		wire := tag & 0x7
		switch {
		case field == 1 && wire == 0:
			v, n := pbVarint(data, pos)
			if n <= 0 {
				return s, fmt.Errorf("excalc: bad varint f1")
			}
			pos += n
			s.Index = int(v)
		case field == 2 && wire == 0:
			v, n := pbVarint(data, pos)
			if n <= 0 {
				return s, fmt.Errorf("excalc: bad varint f2")
			}
			pos += n
			s.Setcode = Setcode(v)
		case field == 3 && wire == 2:
			str, n, err := pbString(data, pos)
			if err != nil {
				return s, err
			}
			pos += n
			s.Code = str
		case field == 4 && wire == 2:
			str, n, err := pbString(data, pos)
			if err != nil {
				return s, err
			}
			pos += n
			s.Name = str
		case field == 5 && wire == 2:
			str, n, err := pbString(data, pos)
			if err != nil {
				return s, err
			}
			pos += n
			s.IndustryCode = str
		case field == 6 && wire == 2:
			str, n, err := pbString(data, pos)
			if err != nil {
				return s, err
			}
			pos += n
			s.IndustryName = str
		default:
			if err := pbSkip(data, &pos, wire); err != nil {
				return s, err
			}
		}
	}
	if s.Code == "" {
		return s, fmt.Errorf("excalc: empty code in stock")
	}
	return s, nil
}

// --- Protobuf helpers ---

// pbVarint reads a varint from data[pos:] and returns (value, bytesRead).
// Returns (0, -1) on error.
func pbVarint(data []byte, pos int) (uint64, int) {
	var v uint64
	shift := 0
	for i := 0; i < 10 && pos+i < len(data); i++ {
		b := data[pos+i]
		v |= uint64(b&0x7f) << shift
		shift += 7
		if b&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, -1
}

// pbString reads a length-prefixed string from data[pos:].
// Returns (value, bytesConsumed, error).
func pbString(data []byte, pos int) (string, int, error) {
	length, n := pbVarint(data, pos)
	if n <= 0 {
		return "", 0, fmt.Errorf("excalc: bad string length varint")
	}
	end := pos + n + int(length)
	if end > len(data) {
		return "", 0, fmt.Errorf("excalc: string out of bounds")
	}
	return string(data[pos+n : end]), n + int(length), nil
}

// pbSkip advances pos past the current field value based on wire type.
func pbSkip(data []byte, pos *int, wire uint64) error {
	switch wire {
	case 0: // varint
		_, n := pbVarint(data, *pos)
		if n <= 0 {
			return fmt.Errorf("excalc: bad skip varint")
		}
		*pos += n
	case 1: // 64-bit
		if *pos+8 > len(data) {
			return fmt.Errorf("excalc: skip 64-bit OOB")
		}
		*pos += 8
	case 2: // length-delimited
		length, n := pbVarint(data, *pos)
		if n <= 0 {
			return fmt.Errorf("excalc: bad skip length")
		}
		*pos += n + int(length)
	case 5: // 32-bit
		if *pos+4 > len(data) {
			return fmt.Errorf("excalc: skip 32-bit OOB")
		}
		*pos += 4
	default:
		return fmt.Errorf("excalc: unknown wire type %d", wire)
	}
	return nil
}
