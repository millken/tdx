package tdx

import (
	"testing"
	"time"
)

// ETF minute bars must come from the main 7709 protocol: the 7727 fund kline
// protocol only carries day-and-above periods, so the fund-code redirect in
// MainClient.GetKlineFrom must not apply to intraday periods.
func TestETFMinuteKlineViaMainClient(t *testing.T) {
	c, err := DialBest(nil, WithTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	for _, code := range []string{"sh510300", "sz159915", "sh588000"} {
		ks, err := c.GetKline(code, Period5Minute, 48)
		if err != nil {
			t.Errorf("%s: %v", code, err)
			continue
		}
		if len(ks) == 0 {
			t.Errorf("%s: got 0 bars, want 5m bars from main protocol", code)
			continue
		}
		last := ks[len(ks)-1]
		if last.Close <= 0 {
			t.Errorf("%s: bad close %v", code, last.Close)
		}
		t.Logf("%s: %d bars, last=%s close=%.3f", code, len(ks),
			last.Time.Format("2006-01-02 15:04"), last.Close)
	}
}

// ETF tick prices must be on the same scale as kline prices: the raw quote
// arrives at 10x stock granularity and DecodeTicks must normalize it via
// transactionPriceScale.
func TestETFTickPriceScale(t *testing.T) {
	c, err := DialBest(nil, WithTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	for _, code := range []string{"sh510300", "sz159915", "sh513180"} {
		tick, err := c.GetTick(code)
		if err != nil {
			t.Errorf("%s tick: %v", code, err)
			continue
		}
		ks, err := c.GetKline(code, Period5Minute, 1)
		if err != nil || len(ks) == 0 {
			t.Errorf("%s kline: %v", code, err)
			continue
		}
		ratio := tick.Price / ks[0].Close
		if ratio < 0.5 || ratio > 2 {
			t.Errorf("%s: tick price %.4f vs kline close %.4f (ratio %.1fx) — scale mismatch",
				code, tick.Price, ks[0].Close, ratio)
			continue
		}
		t.Logf("%s: tick=%.4f kline=%.4f bid1=%.4f ask1=%.4f", code,
			tick.Price, ks[0].Close, tick.BuyLevels[0].Price, tick.SellLevels[0].Price)
	}
}
