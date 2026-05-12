package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/millken/tdx"
)

// 选取有代表性的股票做对比（不同板块、不同价格区间）
var probeStocks = []string{
	"sh000001", // 上证指数（指数）
	"sh600000", // 浦发银行
	"sh600519", // 贵州茅台（高价股）
	"sh601318", // 中国平安
	"sz000001", // 平安银行
	"sz000002", // 万科A
	"sz000858", // 五粮液
	"sz300750", // 宁德时代
	"sz002594", // 比亚迪
	"sz000776", // 广发证券
}

type excalcRow struct {
	Code      string
	TotalMcap float32
	Now       float32
	Change    float32
	FloatMcap float32
}

func main() {
	// 1. 拉取 excalc 数据
	ec := tdx.NewExcalcClient(tdx.ExcalcHost)
	allQuotes, err := ec.GetRealHQ()
	if err != nil {
		fmt.Fprintf(os.Stderr, "GetRealHQ: %v\n", err)
		os.Exit(1)
	}

	// 建立索引
	excalcMap := make(map[string]excalcRow, len(allQuotes))
	for _, q := range allQuotes {
		excalcMap[q.Code] = excalcRow{
			Code:      q.Code,
			TotalMcap: q.TotalMcap,
			Now:       q.YClose,
			Change:    q.Chg3d,
			FloatMcap: q.FloatMcap,
		}
	}

	// 2. 拉取 TDX 7709 BatchQuote 数据
	client, err := tdx.DialBest(tdx.BestSPAddresses(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "DialBest: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	batchQuotes, err := client.GetBatchQuotes(probeStocks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "GetBatchQuotes: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%-8s  %-10s %-12s %-12s %-12s %-12s | %-12s %-14s %-14s %-14s %-14s\n",
		"Code", "NOW(exc)", "Change%(exc)", "field3(exc)", "field12(exc)", "f3-f12",
		"Price(7709)", "Volume(手)", "Amount(元)", "InVol(手)", "OutVol(手)")
	fmt.Println("---------------------------------------------------------------------------------------------------------------------------")

	for _, bq := range batchQuotes {
		code := bq.Code
		er, ok := excalcMap[code]
		if !ok {
			fmt.Printf("%-8s  (no excalc data)\n", code)
			continue
		}

		f3 := float64(er.TotalMcap)
		f12 := float64(er.FloatMcap)
		diff := f3 - f12
		// Amount (成交额 in 元): bq.Amount (float32)
		// Volume (总手): bq.Volume
		// InVol/OutVol (内外盘手)
		fmt.Printf("%-8s  %-10.2f %-12.2f %-12.0f %-12.0f %-12.0f | %-12.2f %-14d %-14.0f %-14d %-14d\n",
			code,
			er.Now,
			er.Change*100,
			f3,
			f12,
			diff,
			bq.Price,
			bq.Volume,
			float64(bq.Amount),
			bq.InsideVolume,
			bq.OutsideVolume,
		)
	}

	fmt.Println()
	fmt.Println("=== 推断验证 ===")
	for _, bq := range batchQuotes {
		code := bq.Code
		er, ok := excalcMap[code]
		if !ok {
			continue
		}
		f3 := float64(er.TotalMcap)
		f12 := float64(er.FloatMcap)
		vol7709 := float64(bq.Volume)       // 总手
		amount7709 := float64(bq.Amount)    // 成交额 元
		inVol := float64(bq.InsideVolume)   // 内盘 手
		outVol := float64(bq.OutsideVolume) // 外盘 手

		fmt.Printf("\n--- %s (price=%.2f) ---\n", code, bq.Price)
		fmt.Printf("  excalc field3  = %15.0f\n", f3)
		fmt.Printf("  excalc field12 = %15.0f\n", f12)
		fmt.Printf("  7709 volume(手)= %15.0f  ratio f3/vol=%.4f  f12/vol=%.4f\n",
			vol7709, f3/vol7709, f12/vol7709)
		fmt.Printf("  7709 amount(元)= %15.0f  ratio f3/amt=%.4f  f12/amt=%.4f\n",
			amount7709, f3/amount7709, f12/amount7709)
		fmt.Printf("  7709 inVol(手) = %15.0f  ratio f3/in=%.4f  f12/in=%.4f\n",
			inVol, f3/inVol, f12/inVol)
		fmt.Printf("  7709 outVol(手)= %15.0f  ratio f3/out=%.4f  f12/out=%.4f\n",
			outVol, f3/outVol, f12/outVol)
		fmt.Printf("  7709 inVol+outVol=%15.0f  ratio f3/(in+out)=%.4f\n",
			inVol+outVol, f3/(inVol+outVol))

		// float32 raw bits
		f3raw := math.Float32bits(er.TotalMcap)
		f12raw := math.Float32bits(er.FloatMcap)
		fmt.Printf("  f3  raw uint32 = %d (0x%08x)\n", f3raw, f3raw)
		fmt.Printf("  f12 raw uint32 = %d (0x%08x)\n", f12raw, f12raw)
		// if raw uint32 interpreted as direct integer
		fmt.Printf("  f3  as uint32  = %-12d  / volume = %.4f  / amount = %.4f\n",
			f3raw, float64(f3raw)/vol7709, float64(f3raw)/amount7709)
		fmt.Printf("  f12 as uint32  = %-12d  / volume = %.4f  / amount = %.4f\n",
			f12raw, float64(f12raw)/vol7709, float64(f12raw)/amount7709)
		_ = binary.LittleEndian
	}

	// 输出 JSON 供进一步分析
	type Row struct {
		Code        string
		ExcTotalMcap float32
		ExcNow      float32
		ExcChange   float32
		ExcFloatMcap float32
		TDXPrice    float64
		TDXVolume   int64
		TDXAmount   float32
		TDXInVol    int64
		TDXOutVol   int64
		TDXPreClose float64
		TDXOpen     float64
		TDXHigh     float64
		TDXLow      float64
	}
	var rows []Row
	for _, bq := range batchQuotes {
		er, ok := excalcMap[bq.Code]
		if !ok {
			continue
		}
		rows = append(rows, Row{
			Code:         bq.Code,
			ExcTotalMcap: er.TotalMcap,
			ExcNow:       er.Now,
			ExcChange:    er.Change,
			ExcFloatMcap: er.FloatMcap,
			TDXPrice:    bq.Price,
			TDXVolume:   bq.Volume,
			TDXAmount:   bq.Amount,
			TDXInVol:    bq.InsideVolume,
			TDXOutVol:   bq.OutsideVolume,
			TDXPreClose: bq.PreClose,
			TDXOpen:     bq.Open,
			TDXHigh:     bq.High,
			TDXLow:      bq.Low,
		})
	}
	f, _ := os.Create("/tmp/excalc_verify.json")
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(rows)
	f.Close()
	fmt.Println("\n详细数据已写入 /tmp/excalc_verify.json")
}
