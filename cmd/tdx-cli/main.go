package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/millken/tdx/tdx"
)

func main() {
	host := flag.String("host", "110.41.147.114:7709", "TDX server host:port")
	code := flag.String("code", "", "stock code (e.g. 600000, sh600000, sz000001)")
	period := flag.String("period", "day", "kline period: 1m|5m|15m|30m|60m|day|week|month|quarter|year")
	count := flag.Int("count", 10, "number of bars (max 800)")
	format := flag.String("format", "table", "output format: table|json|csv")
	out := flag.String("out", "", "output file path (default: stdout)")
	flag.Parse()

	if *code == "" {
		fatalf("code is required, e.g. -code sh600000")
	}

	client, err := tdx.Dial(*host)
	if err != nil {
		fatalf("dial failed: %v", err)
	}
	defer client.Close()

	klines, err := client.GetKline(*code, *period, *count)
	if err != nil {
		fatalf("get kline failed: %v", err)
	}

	writer := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fatalf("create file failed: %v", err)
		}
		defer f.Close()
		writer = f
	}

	switch strings.ToLower(*format) {
	case "table", "":
		renderTable(writer, *code, *period, klines)
	case "json":
		renderJSON(writer, *code, *period, klines)
	case "csv":
		renderCSV(writer, klines)
	default:
		fatalf("unsupported format: %s", *format)
	}
}

func renderTable(w *os.File, code, period string, klines []tdx.Kline) {
	fmt.Fprintf(w, "code=%s period=%s count=%d\n", code, period, len(klines))
	fmt.Fprintf(w, "%-12s %10s %10s %10s %10s %12s %14s\n",
		"Time", "Open", "High", "Low", "Close", "Volume", "Amount")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 80))
	for _, k := range klines {
		fmt.Fprintf(w, "%-12s %10.3f %10.3f %10.3f %10.3f %12s %14s\n",
			k.Time.Format("2006-01-02"),
			k.Open, k.High, k.Low, k.Close,
			tdx.FormatVolume(k.Volume),
			tdx.FormatAmount(k.Amount),
		)
	}
}

func renderJSON(w *os.File, code, period string, klines []tdx.Kline) {
	type output struct {
		Code   string      `json:"code"`
		Period string      `json:"period"`
		Count  int         `json:"count"`
		Items  []tdx.Kline `json:"items"`
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(output{Code: code, Period: period, Count: len(klines), Items: klines})
}

func renderCSV(w *os.File, klines []tdx.Kline) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time", "open", "high", "low", "close", "volume", "amount", "up_count", "down_count"})
	for _, k := range klines {
		_ = cw.Write([]string{
			k.Time.Format("2006-01-02"),
			fmt.Sprintf("%.3f", k.Open),
			fmt.Sprintf("%.3f", k.High),
			fmt.Sprintf("%.3f", k.Low),
			fmt.Sprintf("%.3f", k.Close),
			strconv.FormatInt(k.Volume, 10),
			fmt.Sprintf("%.3f", k.Amount),
			strconv.Itoa(k.UpCount),
			strconv.Itoa(k.DownCount),
		})
	}
	cw.Flush()
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
