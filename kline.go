package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"
)

// Kline represents a single candlestick bar.
type Kline struct {
	Time      time.Time // Bar timestamp
	Open      float64   // Open price (yuan)
	High      float64   // High price (yuan)
	Low       float64   // Low price (yuan)
	Close     float64   // Close price (yuan)
	Volume    int64     // Volume (shares)
	Amount    float64   // Turnover (yuan)
	UpCount   int       // Advancing stocks count (index only)
	DownCount int       // Declining stocks count (index only)
}

// String returns a human-readable representation.
func (k Kline) String() string {
	return fmt.Sprintf("%s O=%.3f H=%.3f L=%.3f C=%.3f V=%d A=%.0f",
		k.Time.Format("2006-01-02 15:04"), k.Open, k.High, k.Low, k.Close, k.Volume, k.Amount)
}

// Period constants for GetKline.
const (
	Period1Minute  = "1m"
	Period5Minute  = "5m"
	Period15Minute = "15m"
	Period30Minute = "30m"
	Period60Minute = "60m"
	PeriodDay      = "day"
	PeriodWeek     = "week"
	PeriodMonth    = "month"
	PeriodQuarter  = "quarter"
	PeriodYear     = "year"
)

// Kline period constants (wire protocol values).
const (
	KlinePeriod5Minute  uint16 = 0
	KlinePeriod15Minute uint16 = 1
	KlinePeriod30Minute uint16 = 2
	KlinePeriod60Minute uint16 = 3
	KlinePeriodDay      uint16 = 4
	KlinePeriodWeek     uint16 = 5
	KlinePeriodMonth    uint16 = 6
	KlinePeriod1Minute  uint16 = 7
	KlinePeriodMultiMin uint16 = 8
	KlinePeriodMultiDay uint16 = 9
	KlinePeriodQuarter  uint16 = 10
	KlinePeriodYear     uint16 = 11
)

// isMinutePeriod reports whether a wire period value is an intraday period.
func isMinutePeriod(period uint16) bool {
	switch period {
	case KlinePeriod1Minute, KlinePeriod5Minute, KlinePeriod15Minute,
		KlinePeriod30Minute, KlinePeriod60Minute, KlinePeriodMultiMin:
		return true
	}
	return false
}

// periodMap maps user-friendly period strings to protocol period values.
var periodMap = map[string]struct {
	period uint16
	times  uint16
}{
	Period1Minute:  {KlinePeriod1Minute, 1},
	Period5Minute:  {KlinePeriod5Minute, 1},
	Period15Minute: {KlinePeriod15Minute, 1},
	Period30Minute: {KlinePeriod30Minute, 1},
	Period60Minute: {KlinePeriod60Minute, 1},
	PeriodDay:      {KlinePeriodDay, 1},
	PeriodWeek:     {KlinePeriodWeek, 1},
	PeriodMonth:    {KlinePeriodMonth, 1},
	PeriodQuarter:  {KlinePeriodQuarter, 1},
	PeriodYear:     {KlinePeriodYear, 1},
}

// Per-request kline limits imposed by the wire format.
const (
	// MaxKlineCount is the most bars a single kline request can return.
	MaxKlineCount = 800

	// MaxKlineStart is the largest start offset the 7709 main quote protocol
	// accepts: the field is two bytes wide (see RequestKLineOffsetFrame), so
	// history beyond this many bars is unreachable. For 5-minute bars that is
	// roughly 65535/48 ≈ 1365 trading days.
	MaxKlineStart = 65535

	// MaxFundKlineStart is the equivalent limit on the 7727 extension protocol,
	// whose start field is four bytes wide (see RequestFundKLineFrame).
	MaxFundKlineStart = math.MaxUint32
)

// GetKline retrieves the most recent K-line bars for the given stock code and
// period. It is shorthand for GetKlineFrom with a zero start offset.
//
// Parameters:
//   - code: stock code, supports "600000", "sh600000", "SH600000" formats
//   - period: one of Period* constants (e.g. "day", "5m", "1m")
//   - count: number of bars to retrieve, max MaxKlineCount
//
// Example:
//
//	klines, err := c.GetKline("sh600000", "day", 100)
func (c *MainClient) GetKline(code string, period string, count int) ([]Kline, error) {
	return c.GetKlineFrom(code, period, 0, count)
}

// GetKlineFrom retrieves K-line bars offset back from the most recent one, so
// callers can page through history deeper than one request can carry.
//
// start counts bars backwards from the latest bar: 0 returns the newest count
// bars, count returns the batch immediately older than those, and so on. Each
// batch is itself in ascending time order.
//
// Paging a full history walks start upwards until a short batch comes back:
//
//	var history []tdx.Kline
//	for start := 0; start <= tdx.MaxKlineStart; start += tdx.MaxKlineCount {
//		batch, err := c.GetKlineFrom(code, tdx.PeriodDay, start, tdx.MaxKlineCount)
//		if err != nil {
//			return nil, err
//		}
//		history = append(batch, history...) // older batch goes in front
//		if len(batch) < tdx.MaxKlineCount {
//			break // reached the earliest bar the server holds
//		}
//	}
func (c *MainClient) GetKlineFrom(code string, period string, start, count int) ([]Kline, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	if count <= 0 || count > MaxKlineCount {
		return nil, fmt.Errorf("tdx: kline count must be 1..%d, got %d", MaxKlineCount, count)
	}
	if start < 0 || start > MaxKlineStart {
		return nil, fmt.Errorf("tdx: kline start must be 0..%d, got %d", MaxKlineStart, start)
	}

	pv, ok := periodMap[strings.ToLower(period)]
	if !ok {
		return nil, fmt.Errorf("tdx: unsupported period %q, use one of: 1m,5m,15m,30m,60m,day,week,month,quarter,year", period)
	}

	normalizedCode, market, _, err := normalizeKlineCode(code)
	if err != nil {
		return nil, err
	}
	if market == 0 {
		market, err = inferKlineMarket(normalizedCode)
		if err != nil {
			return nil, err
		}
	}
	// The 7727 fund kline protocol only carries day-and-above periods; minute
	// bars for exchange-traded funds are served by the main 7709 protocol.
	if isLikelyFundKlineCode(normalizedCode, market) && !isMinutePeriod(pv.period) {
		exClient, err := DialExBest(nil, WithTimeout(c.timeout))
		if err != nil {
			return nil, err
		}
		defer exClient.Close()
		return exClient.GetKlineFrom(normalizedCode, period, start, count)
	}

	kind := inferKlineKind(normalizedCode, market)

	packet, err := RequestKLineOffsetFrame(0x01D20801, 0x01, market, normalizedCode, pv.period, pv.times, uint16(start), uint16(count))
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeKLineOffset, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty kline response")
	}

	return DecodeKlines(response.Body.Decoded, normalizedCode, market, pv.period, kind)
}

// GetKline retrieves the most recent fund K-line bars via the 7727 extension
// quote protocol. It is shorthand for GetKlineFrom with a zero start offset.
func (c *ExClient) GetKline(code string, period string, count int) ([]Kline, error) {
	return c.GetKlineFrom(code, period, 0, count)
}

// GetKlineFrom retrieves fund K-line bars offset back from the most recent one.
// See MainClient.GetKlineFrom for the paging semantics of start.
func (c *ExClient) GetKlineFrom(code string, period string, start, count int) ([]Kline, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	if count <= 0 || count > MaxKlineCount {
		return nil, fmt.Errorf("tdx: kline count must be 1..%d, got %d", MaxKlineCount, count)
	}
	if start < 0 || start > MaxFundKlineStart {
		return nil, fmt.Errorf("tdx: fund kline start must be 0..%d, got %d", MaxFundKlineStart, start)
	}

	pv, ok := periodMap[strings.ToLower(period)]
	if !ok {
		return nil, fmt.Errorf("tdx: unsupported period %q, use one of: 1m,5m,15m,30m,60m,day,week,month,quarter,year", period)
	}

	if _, market, _, err := normalizeKlineCode(code); err == nil && market == MarketBeijing {
		return nil, fmt.Errorf("tdx: fund kline does not support Beijing market code %q", code)
	}

	normalizedCode, _, _, err := normalizeKlineCode(code)
	if err != nil {
		return nil, err
	}

	packet, err := RequestFundKLineFrame(normalizedCode, pv.period, pv.times, uint32(start), uint32(count))
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, FundKlineCMD, c.timeout)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty fund kline response")
	}

	return DecodeFundKlines(response.Body.Decoded, normalizedCode, pv.period)
}

// RequestKLineOffsetFrame builds a 0x052D kline request frame.
func RequestKLineOffsetFrame(msgID uint32, control byte, market uint16, code string, period uint16, times uint16, start uint16, count uint16) ([]byte, error) {
	body := make([]byte, 26)
	binary.LittleEndian.PutUint16(body[0:2], market)
	copy(body[2:8], code)
	binary.LittleEndian.PutUint16(body[8:10], period)
	binary.LittleEndian.PutUint16(body[10:12], times)
	binary.LittleEndian.PutUint16(body[12:14], start)
	binary.LittleEndian.PutUint16(body[14:16], count)
	return BuildDirectFrame(msgID, control, DirectFrameTypeKLineOffset, body), nil
}

// RequestFundKLineFrame builds a 0x2489 fund kline request frame.
func RequestFundKLineFrame(code string, period uint16, times uint16, start uint32, count uint32) ([]byte, error) {
	if len(code) != 6 {
		return nil, fmt.Errorf("tdx: invalid fund code %q", code)
	}
	market, _ := inferKlineMarket(code)
	body := make([]byte, 52)
	body[0] = inferFundCategory(code, market)
	copy(body[1:24], code)
	binary.LittleEndian.PutUint16(body[24:26], period)
	binary.LittleEndian.PutUint16(body[26:28], times)
	binary.LittleEndian.PutUint32(body[28:32], start)
	binary.LittleEndian.PutUint32(body[32:36], count)
	return BuildSPFrame(0x01, FundKlineCMD, body), nil
}

// DecodeKlines decodes a 0x052D kline response body.
func DecodeKlines(body []byte, code string, market uint16, period uint16, kind string) ([]Kline, error) {
	if len(body) < 2 {
		return nil, fmt.Errorf("kline body too short: %d", len(body))
	}

	count := int(binary.LittleEndian.Uint16(body[:2]))
	body = body[2:]
	klines := make([]Kline, 0, count)
	lastCloseMilli := int64(0)

	for i := 0; i < count; i++ {
		if len(body) < 4 {
			return nil, fmt.Errorf("kline record %d missing time bytes", i)
		}

		kline := Kline{Time: decodeKlineTime(body[:4], period)}
		body = body[4:]

		var openDelta, closeDelta, highDelta, lowDelta int64
		var err error
		body, openDelta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("decode open delta record %d: %w", i, err)
		}
		body, closeDelta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("decode close delta record %d: %w", i, err)
		}
		body, highDelta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("decode high delta record %d: %w", i, err)
		}
		body, lowDelta, err = cutPrice(body)
		if err != nil {
			return nil, fmt.Errorf("decode low delta record %d: %w", i, err)
		}

		openMilli := lastCloseMilli + openDelta
		closeMilli := openMilli + closeDelta
		highMilli := openMilli + highDelta
		lowMilli := openMilli + lowDelta

		kline.Open = milliToYuan(openMilli)
		kline.Close = milliToYuan(closeMilli)
		kline.High = milliToYuan(highMilli)
		kline.Low = milliToYuan(lowMilli)
		lastCloseMilli = closeMilli

		if len(body) < 8 {
			return nil, fmt.Errorf("kline record %d missing volume/amount bytes", i)
		}

		volume := int64(decodeTDXVolume(binary.LittleEndian.Uint32(body[:4])))
		body = body[4:]
		if requiresMinuteVolumeScaling(period) {
			volume /= 100
		}
		kline.Volume = volume
		kline.Amount = decodeTDXVolume(binary.LittleEndian.Uint32(body[:4]))
		body = body[4:]

		if kind == "index" {
			if len(body) < 4 {
				return nil, fmt.Errorf("kline index record %d missing breadth bytes", i)
			}
			kline.Volume *= 100
			kline.UpCount = int(binary.LittleEndian.Uint16(body[:2]))
			kline.DownCount = int(binary.LittleEndian.Uint16(body[2:4]))
			body = body[4:]
		}

		klines = append(klines, kline)
	}

	return klines, nil
}

// DecodeFundKlines decodes a 0x2489 fund kline response body.
func DecodeFundKlines(body []byte, code string, period uint16) ([]Kline, error) {
	if len(body) < 42 {
		return nil, fmt.Errorf("fund kline body too short: %d", len(body))
	}

	responsePeriod := binary.LittleEndian.Uint16(body[24:26])
	if responsePeriod != 0 {
		period = responsePeriod
	}
	count := int(binary.LittleEndian.Uint16(body[40:42]))
	pos := 42
	klines := make([]Kline, 0, count)

	for i := 0; i < count; i++ {
		if pos+32 > len(body) {
			return nil, fmt.Errorf("fund kline record %d truncated", i)
		}
		record := body[pos : pos+32]
		kline := Kline{
			Time:   decodeKlineTime(record[0:4], period),
			Open:   float64(math.Float32frombits(binary.LittleEndian.Uint32(record[4:8]))),
			High:   float64(math.Float32frombits(binary.LittleEndian.Uint32(record[8:12]))),
			Low:    float64(math.Float32frombits(binary.LittleEndian.Uint32(record[12:16]))),
			Close:  float64(math.Float32frombits(binary.LittleEndian.Uint32(record[16:20]))),
			Amount: float64(math.Float32frombits(binary.LittleEndian.Uint32(record[20:24]))),
			Volume: int64(binary.LittleEndian.Uint32(record[24:28])),
		}
		klines = append(klines, kline)
		pos += 32
	}

	return klines, nil
}

// FormatVolume formats a volume value with Chinese units (万/亿).
func FormatVolume(n int64) string {
	if n >= 1e8 {
		return fmt.Sprintf("%.2f亿", float64(n)/1e8)
	}
	if n >= 1e4 {
		return fmt.Sprintf("%.2f万", float64(n)/1e4)
	}
	return fmt.Sprintf("%d", n)
}

// FormatAmount formats an amount value with Chinese units (万/亿).
func FormatAmount(f float64) string {
	if f >= 1e8 {
		return fmt.Sprintf("%.2f亿", f/1e8)
	}
	if f >= 1e4 {
		return fmt.Sprintf("%.2f万", f/1e4)
	}
	return fmt.Sprintf("%.2f", f)
}
