package tdx

import (
	"encoding/binary"
	"fmt"
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

// GetKline retrieves K-line data for the given stock code and period.
//
// Parameters:
//   - code: stock code, supports "600000", "sh600000", "SH600000" formats
//   - period: one of Period* constants (e.g. "day", "5m", "1m")
//   - count: number of bars to retrieve, max 800
//
// Example:
//
//	klines, err := c.GetKline("sh600000", "day", 100)
func (c *Client) GetKline(code string, period string, count int) ([]Kline, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	if count <= 0 || count > 800 {
		return nil, fmt.Errorf("tdx: kline count must be 1..800, got %d", count)
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

	kind := inferKlineKind(normalizedCode, market)

	c.drainPending()

	packet, err := RequestKLineOffsetFrame(0x01D20801, 0x01, market, normalizedCode, pv.period, pv.times, 0, uint16(count))
	if err != nil {
		return nil, err
	}
	if err := c.sendRaw(packet); err != nil {
		return nil, err
	}

	response, err := c.waitForCMD(DirectFrameTypeKLineOffset, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty kline response")
	}

	return DecodeKlines(response.Body.Decoded, normalizedCode, market, pv.period, kind)
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
