package tdx

import (
	"fmt"
	"testing"
)

func TestExcalcGetMarketStat(t *testing.T) {
	c := NewExcalcClient(ExcalcHost)
	stat, err := c.GetMarketStat()
	if err != nil {
		t.Fatalf("GetMarketStat: %v", err)
	}
	entries, err := stat.Entries()
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	for _, e := range entries {
		fmt.Printf("  [%d] %s %s close=%s now=%s\n", e.Setcode, e.Code, e.Name, e.Close, e.Now)
	}
}

func TestExcalcGetRealHQ(t *testing.T) {
	c := NewExcalcClient(ExcalcHost)
	quotes, err := c.GetRealHQ()
	if err != nil {
		t.Fatalf("GetRealHQ: %v", err)
	}
	fmt.Printf("Total quotes: %d\n", len(quotes))
	for i, q := range quotes {
		if i >= 5 {
			break
		}
		fmt.Printf("  %s now=%.2f chg=%.2f%% dayclose=%.2f turnover=%.3f%% volratio=%.2f amount=%.0f\n",
			q.Code, q.YClose, q.Chg3d*100, q.Now, q.Turnover, q.VolRatio, q.Amount)
	}
}

func TestExcalcGetCodeTable(t *testing.T) {
	c := NewExcalcClient(ExcalcHost)
	stocks, err := c.GetCodeTable(18)
	if err != nil {
		t.Fatalf("GetCodeTable(18): %v", err)
	}
	fmt.Printf("Total stocks (table 18): %d\n", len(stocks))
	for i, s := range stocks {
		if i >= 5 {
			break
		}
		fmt.Printf("  [%d] %s %s industry=%s(%s)\n", s.Setcode, s.Code, s.Name, s.IndustryCode, s.IndustryName)
	}
}
