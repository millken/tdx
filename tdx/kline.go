package tdx

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/millken/tdx/tdxrpc_new/protocol"
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

// periodMap maps user-friendly period strings to protocol period values.
var periodMap = map[string]struct {
	period uint16
	times  uint16
}{
	Period1Minute:  {protocol.KlinePeriod1Minute, 1},
	Period5Minute:  {protocol.KlinePeriod5Minute, 1},
	Period15Minute: {protocol.KlinePeriod15Minute, 1},
	Period30Minute: {protocol.KlinePeriod30Minute, 1},
	Period60Minute: {protocol.KlinePeriod60Minute, 1},
	PeriodDay:      {protocol.KlinePeriodDay, 1},
	PeriodWeek:     {protocol.KlinePeriodWeek, 1},
	PeriodMonth:    {protocol.KlinePeriodMonth, 1},
	PeriodQuarter:  {protocol.KlinePeriodQuarter, 1},
	PeriodYear:     {protocol.KlinePeriodYear, 1},
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
	p, err := c.ensureProto()
	if err != nil {
		return nil, err
	}

	if count <= 0 || count > 800 {
		return nil, fmt.Errorf("tdx: kline count must be 1..800, got %d", count)
	}

	pv, ok := periodMap[strings.ToLower(period)]
	if !ok {
		return nil, fmt.Errorf("tdx: unsupported period %q, use one of: 1m,5m,15m,30m,60m,day,week,month,quarter,year", period)
	}

	seq, err := p.RequestKline(code,
		protocol.WithKlinePeriod(pv.period, pv.times),
		protocol.WithKlineCount(uint16(count)),
		protocol.WithKlineBootstrap(false), // already bootstrapped in Dial
	)
	if err != nil {
		return nil, fmt.Errorf("tdx: request kline %s %s: %w", code, period, err)
	}

	klines := make([]Kline, 0, count)
	for k := range seq {
		klines = append(klines, Kline{
			Time:      k.Time,
			Open:      k.Open,
			High:      k.High,
			Low:       k.Low,
			Close:     k.Close,
			Volume:    k.Volume,
			Amount:    k.Amount,
			UpCount:   k.UpCount,
			DownCount: k.DownCount,
		})
	}
	return klines, nil
}

// GetKlineByIndex retrieves K-line data with numeric period and times values.
// This is useful for multi-minute or multi-second periods.
//
// Parameters:
//   - code: stock code
//   - period: numeric period value (e.g. 0=5min, 7=1min, 13=multi-sec)
//   - times: times value for the period (e.g. for multi-sec, times=5 means 5-second bars)
//   - count: number of bars, max 800
func (c *Client) GetKlineByIndex(code string, period uint16, times uint16, count int) ([]Kline, error) {
	p, err := c.ensureProto()
	if err != nil {
		return nil, err
	}

	if count <= 0 || count > 800 {
		return nil, fmt.Errorf("tdx: kline count must be 1..800, got %d", count)
	}

	seq, err := p.RequestKline(code,
		protocol.WithKlinePeriod(period, times),
		protocol.WithKlineCount(uint16(count)),
		protocol.WithKlineBootstrap(false),
	)
	if err != nil {
		return nil, fmt.Errorf("tdx: request kline %s period=%d times=%d: %w", code, period, times, err)
	}

	klines := make([]Kline, 0, count)
	for k := range seq {
		klines = append(klines, Kline{
			Time:      k.Time,
			Open:      k.Open,
			High:      k.High,
			Low:       k.Low,
			Close:     k.Close,
			Volume:    k.Volume,
			Amount:    k.Amount,
			UpCount:   k.UpCount,
			DownCount: k.DownCount,
		})
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
	return strconv.FormatInt(n, 10)
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
