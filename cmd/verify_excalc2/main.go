// verify_excalc2: 验证 G_REAL_HQ 未知字段（f7, f9, f11）
// 策略：用 7709 KLine(30日+周K) + BatchQuote 交叉对比
package main

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/millken/tdx"
)

var probeStocks = []struct {
	code7709 string
	code6    string
}{
	{"sz000001", "000001"},
	{"sz000002", "000002"},
	{"sz000006", "000006"},
	{"sh600000", "600000"},
	{"sh600519", "600519"},
	{"sh601318", "601318"}, // 中国平安
	{"sh600036", "600036"}, // 招商银行
	{"sz002475", "002475"}, // 立讯精密
}

func main() {
	ec := tdx.NewExcalcClient(tdx.ExcalcHost)
	allQuotes, err := ec.GetRealHQ()
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetRealHQ:", err)
		os.Exit(1)
	}
	excMap := make(map[string]tdx.StockQuote, len(allQuotes))
	for _, q := range allQuotes {
		excMap[q.Code] = q
	}
	fmt.Printf("excalc: %d stocks loaded\n\n", len(allQuotes))

	client, err := tdx.DialBest(tdx.BestSPAddresses(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "DialBest:", err)
		os.Exit(1)
	}

	for _, s := range probeStocks {
		q, ok := excMap[s.code6]
		if !ok {
			fmt.Printf("[%s] not found in excalc\n", s.code6)
			continue
		}

		// 日K 30根
		kday, err := client.GetKline(s.code7709, "day", 30)
		if err != nil {
			fmt.Printf("[%s] GetKline day: %v\n", s.code6, err)
			continue
		}
		// 周K 10根
		kweek, err := client.GetKline(s.code7709, "week", 10)
		if err != nil {
			fmt.Printf("[%s] GetKline week: %v\n", s.code6, err)
			continue
		}

		bq, err := client.GetBatchQuotes([]string{s.code7709})
		if err != nil || len(bq) == 0 {
			fmt.Printf("[%s] GetBatchQuotes: %v\n", s.code6, err)
			continue
		}
		b := bq[0]

		fmt.Printf("=== %s  NOW=%.3f  f7=%.6f  f9=%.4f  f11=%.2f ===\n",
			s.code6, q.YClose, q.Zangsu, q.VolRatio, q.MainNetAmt)

		// ── f8 确认 ──
		floatShares := float64(q.FloatMcap) / float64(q.YClose)
		floatHand := floatShares / 100
		f8calc := float64(b.Volume) / floatHand * 100 // 换手率% = vol(手)/floatHand * 100
		fmt.Printf("  f8=%.4f%%  calc换手率=%.4f%%  match=%v\n",
			q.Turnover, f8calc, isClose(f8calc, float64(q.Turnover), 0.01))

		// ── f7: 测试各 N 日/周涨幅 ──
		fmt.Printf("  f7=%.6f  ---  N日涨幅测试:\n", q.Zangsu)
		now := float64(q.YClose)
		for n := 2; n < len(kday); n++ {
			ref := kday[len(kday)-1-n].Close
			chg := (now - ref) / ref
			if isClose(chg, float64(q.Zangsu), 0.003) {
				fmt.Printf("    ✓ %d日涨幅(NOW/close[-%d]-1) = %.6f\n", n, n, chg)
			}
		}
		// 用 f5(今日close) 作基准
		f5 := float64(q.Now)
		for n := 1; n <= len(kday)-1; n++ {
			ref := kday[len(kday)-1-n].Close
			chg := (f5 - ref) / ref
			if isClose(chg, float64(q.Zangsu), 0.003) {
				fmt.Printf("    ✓ f5/%d日前close-1 = %.6f\n", n, chg)
			}
		}
		// 周K涨幅
		for n := 1; n < len(kweek); n++ {
			ref := kweek[len(kweek)-1-n].Close
			chg := (now - ref) / ref
			if isClose(chg, float64(q.Zangsu), 0.003) {
				fmt.Printf("    ✓ %d周涨幅(NOW/weekclose[-%d]-1) = %.6f\n", n, n, chg)
			}
		}
		// 也打出最近几条日K和周K供人工对比
		fmt.Printf("    日K(最近5): ")
		for i := len(kday) - 1; i >= len(kday)-6 && i >= 0; i-- {
			fmt.Printf("%s=%.3f  ", kday[i].Time.Format("01/02"), kday[i].Close)
		}
		fmt.Println()
		fmt.Printf("    周K(最近4): ")
		for i := len(kweek) - 1; i >= len(kweek)-5 && i >= 0; i-- {
			fmt.Printf("%s=%.3f  ", kweek[i].Time.Format("01/02"), kweek[i].Close)
		}
		fmt.Println()

		// ── f9: 量比与 BatchQuote 所有相关字段 ──
		fmt.Printf("  f9=%.4f  BatchQuote: VolRatio=%.4f  RiseSpeed=%.4f  Depth=%.4f  Min2Amount=%.0f\n",
			q.VolRatio, b.VolRatio, b.RiseSpeed, b.Depth, b.Min2Amount)
		// 计算今日成交量 / 5日平均量（利用日K）
		if len(kday) >= 6 {
			avg5vol := 0.0
			for i := len(kday) - 6; i < len(kday)-1; i++ {
				avg5vol += float64(kday[i].Volume)
			}
			avg5vol /= 5
			if avg5vol > 0 {
				volRatioCalc := float64(b.Volume) / avg5vol
				fmt.Printf("    calc量比(vol/avg5klineVol)=%.4f  f9=%.4f  match=%v\n",
					volRatioCalc, q.VolRatio, isClose(volRatioCalc, float64(q.VolRatio), 0.1))
			}
		}
		fmt.Printf("    f9/VolRatio=%.3f  f9/Depth=%.3f\n",
			float64(q.VolRatio)/float64(b.VolRatio+1e-9),
			float64(q.VolRatio)/float64(b.Depth+1e-9))

		// ── f11: 各种假设 ──
		fmt.Printf("  f11=%.2f\n", q.MainNetAmt)
		netVolHand := float64(b.InsideVolume - b.OutsideVolume) // 净量(手)
		netAmtYuan := netVolHand * 100 * float64(q.YClose)         // 净额(元)
		fmt.Printf("    in-out净量=%.0f手  净额=%.0f元\n", netVolHand, netAmtYuan)
		fmt.Printf("    净额/万=%.2f  f11/净额(万)=%.4f\n", netAmtYuan/10000, float64(q.MainNetAmt)/(netAmtYuan/10000+1e-9))
		// 试试 f11 = 净量(股) / 某基数
		fmt.Printf("    f11/净量(手)=%.4f  f11/净量(百股)=%.4f\n",
			float64(q.MainNetAmt)/(netVolHand+1e-9),
			float64(q.MainNetAmt)/(netVolHand*100+1e-9))
		// 振幅相关
		amp := (b.High - b.Low) / b.PreClose
		fmt.Printf("    振幅=(H-L)/pre=%.4f  f11*amp=%.4f  f11/(now*amp)=%.4f\n",
			amp, float64(q.MainNetAmt)*amp, float64(q.MainNetAmt)/(float64(q.YClose)*amp+1e-9))
		// 量*价 相关
		fmt.Printf("    Amount/f11=%.4f  f13/f11=%.4f\n",
			float64(b.Amount)/(float64(q.MainNetAmt)+1e-9),
			float64(q.Amount)/(float64(q.MainNetAmt)+1e-9))

		fmt.Println(strings.Repeat("-", 80))
	}
}

func isClose(a, b, tol float64) bool {
	if math.Abs(b) < 1e-9 {
		return math.Abs(a) < tol
	}
	return math.Abs((a-b)/b) < tol
}
