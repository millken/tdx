package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/millken/tdx"
)

// ---------------------------------------------------------------------------
// mac-quotes (0x122D 行情快照)
// ---------------------------------------------------------------------------

func runMACQuotes(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("mac-quotes", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	fs.Parse(args)
	if *code == "" {
		fatalf("mac-quotes: -code is required")
	}

	q, err := client.GetMACQuotes(*code)
	if err != nil {
		fatalf("get mac quotes failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "json":
		b, _ := json.MarshalIndent(q, "", "  ")
		fmt.Fprintln(w, string(b))
	default:
		fmt.Fprintf(w, "%-14s %s\n", "Code", q.Code)
		fmt.Fprintf(w, "%-14s %s\n", "Name", q.Name)
		fmt.Fprintf(w, "%-14s %.2f\n", "Price", q.Price)
		fmt.Fprintf(w, "%-14s %.2f\n", "PreClose", q.PreClose)
		fmt.Fprintf(w, "%-14s %.2f\n", "Open", q.Open)
		fmt.Fprintf(w, "%-14s %.2f\n", "High", q.High)
		fmt.Fprintf(w, "%-14s %.2f\n", "Low", q.Low)
		fmt.Fprintf(w, "%-14s %.2f\n", "Momentum", q.Momentum)
		fmt.Fprintf(w, "%-14s %d\n", "Vol", q.Vol)
		fmt.Fprintf(w, "%-14s %s\n", "Amount", formatFloat(q.Amount))
		fmt.Fprintf(w, "%-14s %s%%\n", "Turnover", formatFloat(q.Turnover))
		fmt.Fprintf(w, "%-14s %s\n", "Avg", formatFloat(q.Avg))
		fmt.Fprintf(w, "%-14s %s\n", "IndustryCode", q.IndustryCode)
		fmt.Fprintf(w, "%-14s %d (chart points)\n", "ChartData", len(q.ChartData))
	}
}

// ---------------------------------------------------------------------------
// mac-symbol-info (0x122A 股票摘要)
// ---------------------------------------------------------------------------

func runMACSymbolInfo(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("mac-symbol-info", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	fs.Parse(args)
	if *code == "" {
		fatalf("mac-symbol-info: -code is required")
	}

	info, err := client.GetMACSymbolInfo(*code)
	if err != nil {
		fatalf("get mac symbol info failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "json":
		b, _ := json.MarshalIndent(info, "", "  ")
		fmt.Fprintln(w, string(b))
	default:
		fmt.Fprintf(w, "%-16s %s\n", "Code", info.Code)
		fmt.Fprintf(w, "%-16s %s\n", "Name", info.Name)
		fmt.Fprintf(w, "%-16s %s\n", "DateTime", info.DateTime.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(w, "%-16s %s\n", "PreClose", formatFloat(info.PreClose))
		fmt.Fprintf(w, "%-16s %s\n", "Open", formatFloat(info.Open))
		fmt.Fprintf(w, "%-16s %s\n", "High", formatFloat(info.High))
		fmt.Fprintf(w, "%-16s %s\n", "Low", formatFloat(info.Low))
		fmt.Fprintf(w, "%-16s %s\n", "Close", formatFloat(info.Close))
		fmt.Fprintf(w, "%-16s %s\n", "Momentum", formatFloat(info.Momentum))
		fmt.Fprintf(w, "%-16s %d\n", "Vol", info.Vol)
		fmt.Fprintf(w, "%-16s %s\n", "Amount", formatFloat(info.Amount))
		fmt.Fprintf(w, "%-16s %d / %d\n", "Inside/Outside", info.InsideVolume, info.OutsideVolume)
		fmt.Fprintf(w, "%-16s %s%%\n", "Turnover", formatFloat(info.Turnover))
		fmt.Fprintf(w, "%-16s %s\n", "Avg", formatFloat(info.Avg))
	}
}

// ---------------------------------------------------------------------------
// mac-symbol-bars (0x122E 统一K线)
// ---------------------------------------------------------------------------

func runMACSymbolBars(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("mac-symbol-bars", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	period := fs.String("period", "day", "period: day|week|month|1m|5m|15m|30m|60m")
	count := fs.Int("count", 20, "number of bars")
	adjust := fs.String("adjust", "none", "adjust: none|qfq(前复权)|hfq(后复权)")
	fs.Parse(args)
	if *code == "" {
		fatalf("mac-symbol-bars: -code is required")
	}

	var adj uint16
	switch strings.ToLower(*adjust) {
	case "", "none":
		adj = tdx.MACAdjustNone
	case "qfq":
		adj = tdx.MACAdjustQFQ
	case "hfq":
		adj = tdx.MACAdjustHFQ
	default:
		fatalf("mac-symbol-bars: unsupported adjust %q", *adjust)
	}

	bars, err := client.GetMACSymbolBars(*code, *period, *count, adj)
	if err != nil {
		fatalf("get mac symbol bars failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "json":
		b, _ := json.MarshalIndent(bars, "", "  ")
		fmt.Fprintln(w, string(b))
	case "csv":
		fmt.Fprintln(w, "DateTime,Open,High,Low,Close,Vol,Amount,RiseRate")
		for _, b := range bars.List {
			fmt.Fprintf(w, "%s,%.2f,%.2f,%.2f,%.2f,%s,%s,%.2f\n",
				b.DateTime.Format("2006-01-02"), b.Open, b.High, b.Low, b.Close,
				formatFloat(b.Vol), formatFloat(b.Amount), b.RiseRate)
		}
	default:
		fmt.Fprintf(w, "%s %s  %d bars\n", bars.Code, bars.Name, len(bars.List))
		fmt.Fprintln(w, "DateTime          Open    High     Low    Close     Vol        Amount  Rise%")
		for _, b := range bars.List {
			fmt.Fprintf(w, "%s %7.2f %7.2f %7.2f %7.2f %8s %12s %6.2f\n",
				b.DateTime.Format("2006-01-02 15:04"), b.Open, b.High, b.Low, b.Close,
				formatFloat(b.Vol), formatFloat(b.Amount), b.RiseRate)
		}
	}
}

// ---------------------------------------------------------------------------
// mac-symbol-quotes (0x122B 批量股票报价)
// ---------------------------------------------------------------------------

func runMACSymbolQuotes(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("mac-symbol-quotes", flag.ExitOnError)
	codes := fs.String("codes", "", "comma-separated stock codes (e.g. sh600000,sz000001)")
	fs.Parse(args)
	if *codes == "" {
		fatalf("mac-symbol-quotes: -codes is required")
	}

	items, err := client.GetMACSymbolQuotes(strings.Split(*codes, ","), nil)
	if err != nil {
		fatalf("get mac symbol quotes failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "json":
		b, _ := json.MarshalIndent(items, "", "  ")
		fmt.Fprintln(w, string(b))
	default:
		fmt.Fprintln(w, "Mkt  Code     Name          Close   PreCls    Open    Chg%    Amount    Turn%   VolR    MainNet")
		for _, it := range items {
			chg := 0.0
			if it.PreClose != 0 {
				chg = float64((it.Close - it.PreClose) / it.PreClose * 100)
			}
			fmt.Fprintf(w, "%-4s %-8s %-12s %7.2f %7.2f %7.2f %7.2f %10s %6.2f%% %6.2f %12s\n",
				tdx.MarketString(it.Market), it.Code, it.Name,
				it.Close, it.PreClose, it.Open, chg,
				formatFloat(float64(it.Amount)), it.Turnover, it.VolRatio, formatFloat(float64(it.MainNetAmount)))
		}
	}
}

// ---------------------------------------------------------------------------
// mac-transactions (0x122F 分时成交)
// ---------------------------------------------------------------------------

func runMACTransactions(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("mac-transactions", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	date := fs.String("date", "", "trade date 20060102 or 2006-01-02 (default: today)")
	count := fs.Int("count", 100, "number of transactions")
	fs.Parse(args)
	if *code == "" {
		fatalf("mac-transactions: -code is required")
	}

	trades, err := client.GetMACTransactions(*code, *date, *count)
	if err != nil {
		fatalf("get mac transactions failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "json":
		b, _ := json.MarshalIndent(trades, "", "  ")
		fmt.Fprintln(w, string(b))
	default:
		fmt.Fprintln(w, "Time          Price     Vol  Count  Buy/Sell")
		for _, t := range trades {
			fmt.Fprintf(w, "%s %8.3f %7d %6d  %d\n", t.Time, t.Price, t.Vol, t.TradeCount, t.BuyOrSell)
		}
	}
}

// ---------------------------------------------------------------------------
// mac-tick-charts (0x123E 多日分时)
// ---------------------------------------------------------------------------

func runMACTickCharts(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("mac-tick-charts", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	date := fs.String("date", "", "start date 20060102 or 2006-01-02 (default: today)")
	days := fs.Int("days", 5, "number of days (1-5)")
	fs.Parse(args)
	if *code == "" {
		fatalf("mac-tick-charts: -code is required")
	}

	tc, err := client.GetMACTickCharts(*code, *date, uint16(*days))
	if err != nil {
		fatalf("get mac tick charts failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "json":
		b, _ := json.MarshalIndent(tc, "", "  ")
		fmt.Fprintln(w, string(b))
	default:
		for _, day := range tc.Charts {
			fmt.Fprintf(w, "=== %s (preClose %.2f) ===\n", day.Date, day.PreClose)
			fmt.Fprintln(w, "Time   Price    Avg     Vol")
			for _, t := range day.Ticks {
				fmt.Fprintf(w, "%s %7.3f %7.3f %6d\n", t.Time, t.Price, t.Avg, t.Vol)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// mac-market-monitor (0x1237 市场监控异动)
// ---------------------------------------------------------------------------

func runMACMarketMonitor(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("mac-market-monitor", flag.ExitOnError)
	market := fs.Int("market", 1, "market: 0=SZ, 1=SH, 2=BJ")
	start := fs.Int("start", 0, "start position")
	count := fs.Int("count", 100, "number of entries")
	fs.Parse(args)

	items, err := client.GetMACMarketMonitor(uint16(*market), uint16(*start), uint16(*count))
	if err != nil {
		fatalf("get mac market monitor failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "json":
		b, _ := json.MarshalIndent(items, "", "  ")
		fmt.Fprintln(w, string(b))
	default:
		fmt.Fprintln(w, "Time      Mkt  Code    Name          Desc         Value")
		for _, it := range items {
			fmt.Fprintf(w, "%s  %-2d %-7s %-12s %-12s %s\n",
				it.Time, it.Market, it.Code, it.Name, it.Desc, it.Value)
		}
	}
}

// ---------------------------------------------------------------------------
// mac-capital-flow (0x1218 资金流向)
// ---------------------------------------------------------------------------

func runMACCapitalFlow(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("mac-capital-flow", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	fs.Parse(args)
	if *code == "" {
		fatalf("mac-capital-flow: -code is required")
	}

	cf, err := client.GetMACCapitalFlow(*code)
	if err != nil {
		fatalf("get mac capital flow failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "json":
		b, _ := json.MarshalIndent(cf, "", "  ")
		fmt.Fprintln(w, string(b))
	default:
		fmt.Fprintf(w, "%-18s %s\n", "QueryInfo", cf.QueryInfo)
		fmt.Fprintf(w, "%-18s %s\n", "Ext", cf.Ext)
		fmt.Fprintln(w, "--- 当日 ---")
		fmt.Fprintf(w, "%-18s %s\n", "MainNetIn", formatFloat(cf.TodayMainNetIn))
		fmt.Fprintf(w, "%-18s %s\n", "MainIn", formatFloat(cf.TodayMainIn))
		fmt.Fprintf(w, "%-18s %s\n", "MainOut", formatFloat(cf.TodayMainOut))
		fmt.Fprintf(w, "%-18s %s\n", "RetailNetIn", formatFloat(cf.TodayRetailNetIn))
		fmt.Fprintln(w, "--- 5日 ---")
		fmt.Fprintf(w, "%-18s %s\n", "MainNetIn5d", formatFloat(cf.FiveDayMainNetIn))
		fmt.Fprintf(w, "%-18s %s\n", "SuperNet5d", formatFloat(cf.FiveDaySuperNet))
		fmt.Fprintf(w, "%-18s %s\n", "LargeNet5d", formatFloat(cf.FiveDayLargeNet))
		fmt.Fprintf(w, "%-18s %s\n", "MediumNet5d", formatFloat(cf.FiveDayMediumNet))
		fmt.Fprintf(w, "%-18s %s\n", "SmallNet5d", formatFloat(cf.FiveDaySmallNet))
	}
}
