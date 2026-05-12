// analyze_excalc_fields: 统计分析 G_REAL_HQ 中 f7/f9/f11 的分布规律
// 用于辅助推断字段含义
package main

import (
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/millken/tdx"
)

func main() {
	ec := tdx.NewExcalcClient(tdx.ExcalcHost)
	all, err := ec.GetRealHQ()
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetRealHQ:", err)
		os.Exit(1)
	}
	fmt.Printf("loaded %d stocks\n\n", len(all))

	var f7s, f9s, f11s []float64
	for _, q := range all {
		f7s = append(f7s, float64(q.Zangsu))
		f9s = append(f9s, float64(q.VolRatio))
		f11s = append(f11s, float64(q.MainNetAmt))
	}

	printStats("f7", f7s)
	printStats("f9", f9s)
	printStats("f11", f11s)

	// 观察 f9 与 f10(Change) 的相关性
	// 以及 f7 与 f10 的相关性
	var (
		sumF7Change, sumF9Change float64
		sumF7_2, sumF9_2         float64
		sumChange_2              float64
		n                        int
	)
	for _, q := range all {
		f7 := float64(q.Zangsu)
		f9 := float64(q.VolRatio)
		ch := float64(q.Chg3d)
		sumF7Change += f7 * ch
		sumF9Change += f9 * ch
		sumF7_2 += f7 * f7
		sumF9_2 += f9 * f9
		sumChange_2 += ch * ch
		n++
	}
	if sumF7_2 > 0 && sumChange_2 > 0 {
		corrF7 := sumF7Change / math.Sqrt(sumF7_2*sumChange_2)
		corrF9 := sumF9Change / math.Sqrt(sumF9_2*sumChange_2)
		fmt.Printf("Correlation f7 vs Change(f10): %.4f\n", corrF7)
		fmt.Printf("Correlation f9 vs Change(f10): %.4f\n\n", corrF9)
	}

	// f9 与 f8(换手率%) 的相关性
	var sumF9F8, sumF8_2 float64
	for _, q := range all {
		f9 := float64(q.VolRatio)
		f8 := float64(q.Turnover)
		sumF9F8 += f9 * f8
		sumF8_2 += f8 * f8
	}
	if sumF9_2 > 0 && sumF8_2 > 0 {
		corrF9F8 := sumF9F8 / math.Sqrt(sumF9_2*sumF8_2)
		fmt.Printf("Correlation f9 vs f8(换手率): %.4f\n\n", corrF9F8)
	}

	// f11 的符号分布
	pos, neg, zero := 0, 0, 0
	for _, v := range f11s {
		if v > 0.01 {
			pos++
		} else if v < -0.01 {
			neg++
		} else {
			zero++
		}
	}
	fmt.Printf("f11 符号分布: 正=%d  负=%d  零=%d  正/总=%.2f%%\n\n", pos, neg, zero, float64(pos)/float64(n)*100)

	// f9 的符号
	posF9, negF9 := 0, 0
	for _, v := range f9s {
		if v >= 0 {
			posF9++
		} else {
			negF9++
		}
	}
	fmt.Printf("f9 符号分布: 正=%d  负=%d\n\n", posF9, negF9)

	// f7 与 f6 的关系
	var sumF7F6, sumF6_2, sumF7_22 float64
	for _, q := range all {
		f7 := float64(q.Zangsu)
		f6 := float64(q.DayChgPct)
		sumF7F6 += f7 * f6
		sumF6_2 += f6 * f6
		sumF7_22 += f7 * f7
	}
	if sumF7_22 > 0 && sumF6_2 > 0 {
		corrF7F6 := sumF7F6 / math.Sqrt(sumF7_22*sumF6_2)
		fmt.Printf("Correlation f7 vs f6: %.4f\n", corrF7F6)
	}

	// f7 与 f10 的符号一致性
	sameSign, diffSign := 0, 0
	for _, q := range all {
		f7 := float64(q.Zangsu)
		f10 := float64(q.Chg3d)
		if (f7 > 0 && f10 > 0) || (f7 < 0 && f10 < 0) {
			sameSign++
		} else if f7 != 0 && f10 != 0 {
			diffSign++
		}
	}
	fmt.Printf("f7 vs f10 符号相同/不同: %d / %d  (相同比例=%.1f%%)\n", sameSign, diffSign, float64(sameSign)/float64(sameSign+diffSign)*100)
}

func printStats(name string, vs []float64) {
	if len(vs) == 0 {
		return
	}
	sorted := make([]float64, len(vs))
	copy(sorted, vs)
	sort.Float64s(sorted)

	sum, sum2 := 0.0, 0.0
	for _, v := range vs {
		sum += v
		sum2 += v * v
	}
	mean := sum / float64(len(vs))
	variance := sum2/float64(len(vs)) - mean*mean
	std := math.Sqrt(variance)
	n := len(sorted)

	fmt.Printf("=== %s (%d stocks) ===\n", name, n)
	fmt.Printf("  min=%.4f  p1=%.4f  p5=%.4f  p25=%.4f  median=%.4f  p75=%.4f  p95=%.4f  p99=%.4f  max=%.4f\n",
		sorted[0],
		sorted[n/100],
		sorted[n/20],
		sorted[n/4],
		sorted[n/2],
		sorted[3*n/4],
		sorted[19*n/20],
		sorted[99*n/100],
		sorted[n-1],
	)
	fmt.Printf("  mean=%.4f  std=%.4f\n\n", mean, std)
}
