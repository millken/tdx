package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/millken/tdx"
)

func main() {
	host := flag.String("host", "", "TDX market data server host:port (auto-detect if empty)")
	spHost := flag.String("sp-host", "", "TDX mac_quotation server host:port (auto-detect if empty)")
	poolSize := flag.Int("pool-size", 0, "connection pool size (0=disabled)")
	format := flag.String("format", "table", "output format: table|json|csv")
	out := flag.String("out", "", "output file path (default: stdout)")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	cmd := args[0]
	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			fatalf("create file failed: %v", err)
		}
		defer f.Close()
		w = f
	}

	switch cmd {
	case "probe":
		runProbe(args[1:])
		return
	}

	// Determine if we need SP mode
	spCmd := cmd == "limit" || cmd == "board-members"

	// Build pool or single client
	if *poolSize > 0 {
		var pool *tdx.Pool
		var err error
		opts := []tdx.Option{}
		if spCmd {
			opts = append(opts, tdx.WithSP())
		}
		pool, err = tdx.NewPool(*poolSize, opts...)
		if err != nil {
			fatalf("create pool failed: %v", err)
		}
		defer pool.Close()

		client := pool.Get()
		runCommand(cmd, client, args[1:], w, *format)
	} else if spCmd {
		var client *tdx.MainClient
		var err error
		if *spHost != "" {
			client, err = tdx.DialSP(*spHost)
		} else {
			client, err = tdx.DialSPBest(nil)
		}
		if err != nil {
			fatalf("dial SP host failed: %v", err)
		}
		defer client.Close()
		runCommand(cmd, client, args[1:], w, *format)
	} else {
		var client *tdx.MainClient
		var err error
		if *host != "" {
			client, err = tdx.Dial(*host)
		} else {
			client, err = tdx.DialBest(nil)
		}
		if err != nil {
			fatalf("dial host failed: %v", err)
		}
		defer client.Close()
		runCommand(cmd, client, args[1:], w, *format)
	}
}

func runCommand(cmd string, client *tdx.Client, args []string, w *os.File, format string) {
	switch cmd {
	case "kline":
		runKline(client, args, w, format)
	case "tick":
		runTick(client, args, w, format)
	case "tickchart":
		runTickChart(client, args, w, format)
	case "transaction":
		runTransaction(client, args, w, format)
	case "history-trade":
		runHistoryTrade(client, args, w, format)
	case "codelist":
		runCodeList(client, args, w, format)
	case "finance":
		runFinance(client, args, w, format)
	case "xdxr":
		runXdXr(client, args, w, format)
	case "company":
		runCompany(client, args, w, format)
	case "company-content":
		runCompanyContent(client, args, w, format)
	case "topboard":
		runTopBoard(client, args, w, format)
	case "quotes":
		runQuotes(client, args, w, format)
	case "lhb":
		runLHB(client, args, w, format)
	case "ticks":
		runTicks(client, args, w, format)
	case "bq":
		runBatchQuote(client, args, w, format)
	case "limit":
		runLimit(client, args, w, format)
	case "board-members":
		runBoardMembers(client, args, w, format)
	default:
		fatalf("unknown command: %s\n\n%s", cmd, availableCommands())
	}
}

// ---------------------------------------------------------------------------
// kline
// ---------------------------------------------------------------------------

func runKline(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("kline", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	period := fs.String("period", "day", "kline period: 1m|5m|15m|30m|60m|day|week|month|quarter|year")
	count := fs.Int("count", 10, "number of bars (max 800)")
	fs.Parse(args)

	if *code == "" {
		fatalf("kline: -code is required")
	}

	klines, err := client.GetKline(*code, *period, *count)
	if err != nil {
		fatalf("get kline failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderKlineTable(w, *code, *period, klines)
	case "json":
		renderKlineJSON(w, *code, *period, klines)
	case "csv":
		renderKlineCSV(w, klines)
	default:
		fatalf("unsupported format: %s", format)
	}
}

func renderKlineTable(w *os.File, code, period string, klines []tdx.Kline) {
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

func renderKlineJSON(w *os.File, code, period string, klines []tdx.Kline) {
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

func renderKlineCSV(w *os.File, klines []tdx.Kline) {
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

// ---------------------------------------------------------------------------
// tick
// ---------------------------------------------------------------------------

func runTick(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("tick", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	fs.Parse(args)

	if *code == "" {
		fatalf("tick: -code is required")
	}

	tick, err := client.GetTick(*code)
	if err != nil {
		fatalf("get tick failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderTickTable(w, tick)
	case "json":
		renderTickJSON(w, tick)
	default:
		fatalf("tick: unsupported format: %s", format)
	}
}

func renderTickTable(w *os.File, t *tdx.Tick) {
	fmt.Fprintf(w, "code=%s market=%s\n", t.Code, tdx.MarketString(t.Market))
	fmt.Fprintf(w, "%-14s %10s\n", "Field", "Value")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 26))
	fmt.Fprintf(w, "%-14s %10.3f\n", "PrevClose", t.PrevClose)
	fmt.Fprintf(w, "%-14s %10.3f\n", "Open", t.Open)
	fmt.Fprintf(w, "%-14s %10.3f\n", "High", t.High)
	fmt.Fprintf(w, "%-14s %10.3f\n", "Low", t.Low)
	fmt.Fprintf(w, "%-14s %10.3f\n", "Price", t.Price)
	fmt.Fprintf(w, "%-14s %10s\n", "TotalVolume", tdx.FormatVolume(t.TotalVolume))
	fmt.Fprintf(w, "%-14s %10s\n", "Volume", tdx.FormatVolume(t.Volume))
	fmt.Fprintf(w, "%-14s %10s\n", "Amount", tdx.FormatAmount(t.Amount))
	fmt.Fprintf(w, "%-14s %10.2f%%\n", "ChangeRate", t.CalcChangeRate())

	fmt.Fprintf(w, "\n%-6s %10s %10s | %10s %10s\n", "Level", "BuyPrice", "BuyVol", "SellPrice", "SellVol")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 52))
	for i := 0; i < 5; i++ {
		fmt.Fprintf(w, "%-6d %10.3f %10s | %10.3f %10s\n",
			i+1,
			t.BuyLevels[i].Price, tdx.FormatVolume(t.BuyLevels[i].Volume),
			t.SellLevels[i].Price, tdx.FormatVolume(t.SellLevels[i].Volume),
		)
	}
}

func renderTickJSON(w *os.File, t *tdx.Tick) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(t)
}

// ---------------------------------------------------------------------------
// tickchart
// ---------------------------------------------------------------------------

func runTickChart(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("tickchart", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	count := fs.Int("count", 240, "number of data points")
	fs.Parse(args)

	if *code == "" {
		fatalf("tickchart: -code is required")
	}

	chart, err := client.GetTickChart(*code, *count)
	if err != nil {
		fatalf("get tickchart failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderTickChartTable(w, chart)
	case "json":
		renderTickChartJSON(w, chart)
	case "csv":
		renderTickChartCSV(w, chart)
	default:
		fatalf("tickchart: unsupported format: %s", format)
	}
}

func renderTickChartTable(w *os.File, chart []tdx.TickChart) {
	fmt.Fprintf(w, "%-8s %10s %12s %10s\n", "Time", "Price", "Avg", "Volume")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 42))
	for _, p := range chart {
		fmt.Fprintf(w, "%-8s %10.2f %12.4f %10d\n", p.Time, p.Price, p.Avg, p.Volume)
	}
}

func renderTickChartJSON(w *os.File, chart []tdx.TickChart) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(chart)
}

func renderTickChartCSV(w *os.File, chart []tdx.TickChart) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time", "price", "avg", "volume"})
	for _, p := range chart {
		_ = cw.Write([]string{
			p.Time,
			fmt.Sprintf("%.2f", p.Price),
			fmt.Sprintf("%.4f", p.Avg),
			strconv.FormatInt(p.Volume, 10),
		})
	}
	cw.Flush()
}

// ---------------------------------------------------------------------------
// transaction
// ---------------------------------------------------------------------------

func runTransaction(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("transaction", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	date := fs.String("date", "", "trade date: yyyyMMdd or yyyy-MM-dd (default: today)")
	count := fs.Int("count", 100, "number of records (max 900)")
	fs.Parse(args)

	if *code == "" {
		fatalf("transaction: -code is required")
	}

	trades, err := client.GetTransaction(*code, *date, *count)
	if err != nil {
		fatalf("get transaction failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderTransactionTable(w, trades)
	case "json":
		renderTransactionJSON(w, trades)
	case "csv":
		renderTransactionCSV(w, trades)
	default:
		fatalf("transaction: unsupported format: %s", format)
	}
}

func renderTransactionTable(w *os.File, trades []tdx.Transaction) {
	fmt.Fprintf(w, "%-8s %10s %10s %10s %6s\n", "Time", "Price", "Volume", "Number", "Status")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 46))
	for _, t := range trades {
		fmt.Fprintf(w, "%-8s %10.3f %10d %10d %6d\n",
			t.Time.Format("15:04"),
			t.Price, t.Volume, t.Number, t.Status,
		)
	}
}

func renderTransactionJSON(w *os.File, trades []tdx.Transaction) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(trades)
}

func renderTransactionCSV(w *os.File, trades []tdx.Transaction) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time", "price", "volume", "number", "status"})
	for _, t := range trades {
		_ = cw.Write([]string{
			t.Time.Format("15:04"),
			fmt.Sprintf("%.3f", t.Price),
			strconv.FormatInt(t.Volume, 10),
			strconv.FormatInt(t.Number, 10),
			strconv.FormatInt(t.Status, 10),
		})
	}
	cw.Flush()
}

// ---------------------------------------------------------------------------
// history-trade
// ---------------------------------------------------------------------------

func runHistoryTrade(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("history-trade", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	date := fs.String("date", "", "trade date: yyyyMMdd or yyyy-MM-dd (required)")
	count := fs.Int("count", 500, "number of records (max 2000)")
	fs.Parse(args)

	if *code == "" {
		fatalf("history-trade: -code is required")
	}
	if *date == "" {
		fatalf("history-trade: -date is required")
	}

	trades, err := client.GetHistoryTrade(*date, *code, *count)
	if err != nil {
		fatalf("get history trade failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderHistoryTradeTable(w, trades)
	case "json":
		renderHistoryTradeJSON(w, trades)
	case "csv":
		renderHistoryTradeCSV(w, trades)
	default:
		fatalf("history-trade: unsupported format: %s", format)
	}
}

func renderHistoryTradeTable(w *os.File, trades []tdx.HistoryTrade) {
	fmt.Fprintf(w, "%-8s %10s %10s %6s\n", "Time", "Price", "Volume", "Status")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 36))
	for _, t := range trades {
		fmt.Fprintf(w, "%-8s %10.3f %10d %6d\n",
			t.Time.Format("15:04"),
			t.Price, t.Volume, t.Status,
		)
	}
}

func renderHistoryTradeJSON(w *os.File, trades []tdx.HistoryTrade) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(trades)
}

func renderHistoryTradeCSV(w *os.File, trades []tdx.HistoryTrade) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time", "price", "volume", "status"})
	for _, t := range trades {
		_ = cw.Write([]string{
			t.Time.Format("15:04"),
			fmt.Sprintf("%.3f", t.Price),
			strconv.FormatInt(t.Volume, 10),
			strconv.FormatInt(t.Status, 10),
		})
	}
	cw.Flush()
}

// ---------------------------------------------------------------------------
// codelist
// ---------------------------------------------------------------------------

func runCodeList(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("codelist", flag.ExitOnError)
	market := fs.Int("market", 1, "market: 0=shenzhen, 1=shanghai, 2=beijing")
	start := fs.Int("start", 0, "page offset (0, 1000, 2000, ...)")
	fs.Parse(args)

	page, err := client.GetMarketCodes(uint16(*market), uint16(*start))
	if err != nil {
		fatalf("get codelist failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderCodeListTable(w, page)
	case "json":
		renderCodeListJSON(w, page)
	case "csv":
		renderCodeListCSV(w, page)
	default:
		fatalf("codelist: unsupported format: %s", format)
	}
}

func renderCodeListTable(w *os.File, page *tdx.MarketCodePage) {
	fmt.Fprintf(w, "market=%s count=%d\n", tdx.MarketString(page.List[0].Market), page.Count)
	fmt.Fprintf(w, "%-8s %-8s %-10s %6s %4s %10s\n",
		"Code", "Market", "Name", "Multi", "Dec", "LastPrice")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 50))
	for _, c := range page.List {
		fmt.Fprintf(w, "%-8s %-8d %-10s %6d %4d %10.3f\n",
			c.Code, c.Market, c.Name, c.Multiple, c.Decimal, c.LastPrice,
		)
	}
}

func renderCodeListJSON(w *os.File, page *tdx.MarketCodePage) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(page)
}

func renderCodeListCSV(w *os.File, page *tdx.MarketCodePage) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"code", "market", "name", "multiple", "decimal", "last_price"})
	for _, c := range page.List {
		_ = cw.Write([]string{
			c.Code,
			strconv.FormatUint(uint64(c.Market), 10),
			c.Name,
			strconv.FormatUint(uint64(c.Multiple), 10),
			strconv.FormatInt(int64(c.Decimal), 10),
			fmt.Sprintf("%.3f", c.LastPrice),
		})
	}
	cw.Flush()
}

// ---------------------------------------------------------------------------
// finance
// ---------------------------------------------------------------------------

func runFinance(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("finance", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	fs.Parse(args)

	if *code == "" {
		fatalf("finance: -code is required")
	}

	info, err := client.GetFinance(*code)
	if err != nil {
		fatalf("get finance failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderFinanceTable(w, info)
	case "json":
		renderFinanceJSON(w, info)
	default:
		fatalf("finance: unsupported format: %s", format)
	}
}

func renderFinanceTable(w *os.File, info *tdx.FinanceInfo) {
	fmt.Fprintf(w, "%-18s %14s\n", "Field", "Value")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 34))
	fmt.Fprintf(w, "%-18s %14s\n", "LiuTongGuBen", formatFloat(info.LiuTongGuBen))
	fmt.Fprintf(w, "%-18s %14d\n", "Province", info.Province)
	fmt.Fprintf(w, "%-18s %14d\n", "Industry", info.Industry)
	fmt.Fprintf(w, "%-18s %14d\n", "UpdatedDate", info.UpdatedDate)
	fmt.Fprintf(w, "%-18s %14d\n", "IPODate", info.IPODate)
	fmt.Fprintf(w, "%-18s %14s\n", "ZongGuBen", formatFloat(info.ZongGuBen))
	fmt.Fprintf(w, "%-18s %14s\n", "GuoJiaGu", formatFloat(info.GuoJiaGu))
	fmt.Fprintf(w, "%-18s %14s\n", "BGu", formatFloat(info.BGu))
	fmt.Fprintf(w, "%-18s %14s\n", "HGu", formatFloat(info.HGu))
	fmt.Fprintf(w, "%-18s %14s\n", "MeiGuShouYi", formatFloat(info.MeiGuShouYi))
	fmt.Fprintf(w, "%-18s %14s\n", "ZiChanZongJi", formatFloat(info.ZiChanZongJi))
	fmt.Fprintf(w, "%-18s %14s\n", "LiuDongZiChan", formatFloat(info.LiuDongZiChanZongJi))
	fmt.Fprintf(w, "%-18s %14s\n", "GuDongRenShu", formatFloat(info.GuDongRenShu))
	fmt.Fprintf(w, "%-18s %14s\n", "LiuDongFuZhai", formatFloat(info.LiuDongFuZhaiHeJi))
	fmt.Fprintf(w, "%-18s %14s\n", "ZiBenGongJiJin", formatFloat(info.ZiBenGongJiJin))
	fmt.Fprintf(w, "%-18s %14s\n", "GuiMoQuanYiHeJi", formatFloat(info.GuiMoQuanYiHeJi))
	fmt.Fprintf(w, "%-18s %14s\n", "YinYeZongShouRu", formatFloat(info.YinYeZongShouRu))
	fmt.Fprintf(w, "%-18s %14s\n", "YinYeLiRun", formatFloat(info.YinYeLiRun))
	fmt.Fprintf(w, "%-18s %14s\n", "JingYingXianJinLiu", formatFloat(info.JingYingXianJinLiu))
	fmt.Fprintf(w, "%-18s %14s\n", "LiRunZongE", formatFloat(info.LiRunZongE))
	fmt.Fprintf(w, "%-18s %14s\n", "JingLiRun", formatFloat(info.GuiMoJinLiRun))
	fmt.Fprintf(w, "%-18s %14s\n", "WeiFenLiRun", formatFloat(info.WeiFenLiRun))
	fmt.Fprintf(w, "%-18s %14s\n", "MeiGuJingZiChan", formatFloat(info.MeiGuJingZiChan))
}

func renderFinanceJSON(w *os.File, info *tdx.FinanceInfo) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(info)
}

// ---------------------------------------------------------------------------
// xdxr
// ---------------------------------------------------------------------------

func runXdXr(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("xdxr", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	fs.Parse(args)

	if *code == "" {
		fatalf("xdxr: -code is required")
	}

	items, err := client.GetXdXr(*code)
	if err != nil {
		fatalf("get xdxr failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderXdXrTable(w, items)
	case "json":
		renderXdXrJSON(w, items)
	default:
		fatalf("xdxr: unsupported format: %s", format)
	}
}

func renderXdXrTable(w *os.File, items []tdx.XdXrItem) {
	fmt.Fprintf(w, "%-12s %-10s %10s %10s %10s %10s\n",
		"Date", "Category", "FenHong", "PeiGuJia", "SongZhuanGu", "PeiGu")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 64))
	for _, item := range items {
		fmt.Fprintf(w, "%-12s %-10s %10.4f %10.4f %10.4f %10.4f\n",
			item.Date.Format("2006-01-02"),
			tdx.XdXrCategoryString(item.Category),
			item.FenHong, item.PeiGuJia, item.SongZhuanGu, item.PeiGu,
		)
	}
}

func renderXdXrJSON(w *os.File, items []tdx.XdXrItem) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(items)
}

// ---------------------------------------------------------------------------
// company
// ---------------------------------------------------------------------------

func runCompany(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("company", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	fs.Parse(args)

	if *code == "" {
		fatalf("company: -code is required")
	}

	items, err := client.GetCompanyCategory(*code)
	if err != nil {
		fatalf("get company category failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderCompanyTable(w, items)
	case "json":
		renderCompanyJSON(w, items)
	case "csv":
		renderCompanyCSV(w, items)
	default:
		fatalf("company: unsupported format: %s", format)
	}
}

func renderCompanyTable(w *os.File, items []tdx.CompanyCategoryItem) {
	fmt.Fprintf(w, "%-20s %-30s %8s %8s\n", "Name", "Filename", "Start", "Length")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 68))
	for _, c := range items {
		fmt.Fprintf(w, "%-20s %-30s %8d %8d\n", c.Name, c.Filename, c.Start, c.Length)
	}
}

func renderCompanyJSON(w *os.File, items []tdx.CompanyCategoryItem) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(items)
}

func renderCompanyCSV(w *os.File, items []tdx.CompanyCategoryItem) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"name", "filename", "start", "length"})
	for _, c := range items {
		_ = cw.Write([]string{
			c.Name, c.Filename,
			strconv.FormatUint(uint64(c.Start), 10),
			strconv.FormatUint(uint64(c.Length), 10),
		})
	}
	cw.Flush()
}

// ---------------------------------------------------------------------------
// company-content
// ---------------------------------------------------------------------------

func runCompanyContent(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("company-content", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	filename := fs.String("filename", "", "section filename (required)")
	start := fs.Uint("start", 0, "content start offset")
	length := fs.Uint("length", 10000, "content length")
	fs.Parse(args)

	if *code == "" {
		fatalf("company-content: -code is required")
	}
	if *filename == "" {
		fatalf("company-content: -filename is required")
	}

	content, err := client.GetCompanyContent(*code, *filename, uint32(*start), uint32(*length))
	if err != nil {
		fatalf("get company content failed: %v", err)
	}

	fmt.Fprint(w, content)
}

func formatFloat(v float64) string {
	if v == 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", v)
}

// ---------------------------------------------------------------------------
// topboard
// ---------------------------------------------------------------------------

func runTopBoard(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("topboard", flag.ExitOnError)
	market := fs.Int("market", 6, "market: 0=SZ, 1=SH, 2=BJ, 6=A股, 8=科创板, 12=北证, 14=创业板")
	size := fs.Int("size", 20, "entries per list")
	board := fs.String("board", "increase", "board: increase|decrease|amplitude|rise_speed|fall_speed|vol_ratio|turnover")
	fs.Parse(args)

	result, err := client.GetTopBoard(uint16(*market), *size)
	if err != nil {
		fatalf("get topboard failed: %v", err)
	}

	var items []tdx.TopBoardItem
	switch *board {
	case "decrease":
		items = result.Decrease
	case "amplitude":
		items = result.Amplitude
	case "rise_speed":
		items = result.RiseSpeed
	case "fall_speed":
		items = result.FallSpeed
	case "vol_ratio":
		items = result.VolRatio
	case "turnover":
		items = result.Turnover
	default:
		items = result.Increase
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderTopBoardTable(w, *board, items)
	case "json":
		renderTopBoardJSON(w, *board, items)
	default:
		fatalf("topboard: unsupported format: %s", format)
	}
}

func renderTopBoardTable(w *os.File, board string, items []tdx.TopBoardItem) {
	fmt.Fprintf(w, "%-8s %-6s %10s %12s\n", "Board", "Market", "Price", "Value")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 38))
	for _, item := range items {
		fmt.Fprintf(w, "%-8s %-6s %10.2f %12.4f\n", board, tdx.MarketString(item.Market), item.Price, item.Value)
	}
}

func renderTopBoardJSON(w *os.File, board string, items []tdx.TopBoardItem) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string][]tdx.TopBoardItem{board: items})
}

// ---------------------------------------------------------------------------
// quotes
// ---------------------------------------------------------------------------

func runQuotes(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("quotes", flag.ExitOnError)
	market := fs.Int("market", 6, "market: 0=SZ, 1=SH, 2=BJ, 6=A股, 7=B股, 8=科创板, 12=北证, 14=创业板")
	sortStr := fs.String("sort", "change_pct", "sort field: change_pct|amplitude_pct|turnover_rate|vol_ratio|speed_pct|code|price|volume|amount|pe|entrust|inout|locked_ratio|locked_amount|float_mcap|total_mcap|strength|activity|short_turnover|vol_speed|main_net|amount_2m")
	count := fs.Int("count", 20, "number of stocks")
	reverse := fs.Bool("reverse", false, "sort ascending")
	filterStr := fs.String("filter", "", "exclude types (comma OR): new|kcb|st|cyb|bj")
	fs.Parse(args)

	sortType := tdx.SortChangePct
	switch *sortStr {
	case "price":
		sortType = tdx.SortPrice
	case "volume":
		sortType = tdx.SortVolume
	case "amount":
		sortType = tdx.SortAmount
	case "amplitude_pct":
		sortType = tdx.SortAmplitudePct
	case "pe":
		sortType = tdx.SortPEDynamic
	case "entrust":
		sortType = tdx.SortEntrustRatio
	case "inout":
		sortType = tdx.SortInOutRatio
	case "locked_ratio":
		sortType = tdx.SortLockedRatio
	case "locked_amount":
		sortType = tdx.SortLockedAmount
	case "turnover_rate":
		sortType = tdx.SortTurnoverRate
	case "vol_ratio":
		sortType = tdx.SortVolRatio
	case "float_mcap":
		sortType = tdx.SortFloatMcap
	case "total_mcap":
		sortType = tdx.SortTotalMcapAB
	case "strength":
		sortType = tdx.SortStrengthPct
	case "speed_pct":
		sortType = tdx.SortSpeedPct
	case "activity":
		sortType = tdx.SortActivity
	case "short_turnover":
		sortType = tdx.SortShortTurnover
	case "vol_speed":
		sortType = tdx.SortVolSpeedPct
	case "main_net":
		sortType = tdx.SortMainNetAmount
	case "amount_2m":
		sortType = tdx.SortAmount2M
	case "code":
		sortType = tdx.SortCode
	}

	var excludes []uint16
	if *filterStr != "" {
		filterMap := map[string]uint16{
			"new": tdx.FilterNew,
			"kcb": tdx.FilterKCB,
			"st":  tdx.FilterST,
			"cyb": tdx.FilterCYB,
			"bj":  tdx.FilterBJ,
		}
		for _, f := range strings.Split(*filterStr, ",") {
			f = strings.TrimSpace(f)
			if v, ok := filterMap[f]; ok {
				excludes = append(excludes, v)
			} else {
				fatalf("quotes: unknown filter %q (valid: new|kcb|st|cyb|bj)", f)
			}
		}
	}

	items, err := client.GetQuotesList(uint16(*market), sortType, 0, *count, *reverse, excludes...)
	if err != nil {
		fatalf("get quotes failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderQuotesTable(w, items)
	case "json":
		renderQuotesJSON(w, items)
	case "csv":
		renderQuotesCSV(w, items)
	default:
		fatalf("quotes: unsupported format: %s", format)
	}
}

func renderQuotesTable(w *os.File, items []tdx.QuotesItem) {
	fmt.Fprintf(w, "%-6s %-8s %10s %10s %10s %10s %10s %12s %8s\n",
		"Mkt", "Code", "Price", "Open", "High", "Low", "PreClose", "Amount", "Active")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 96))
	for _, q := range items {
		fmt.Fprintf(w, "%-6s %-8s %10.2f %10.2f %10.2f %10.2f %10.2f %12.0f %8d\n",
			tdx.MarketString(q.Market), q.Code, q.Price, q.Open, q.High, q.Low, q.PreClose, q.Amount, q.Active)
	}
}

func renderQuotesJSON(w *os.File, items []tdx.QuotesItem) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(items)
}

func renderQuotesCSV(w *os.File, items []tdx.QuotesItem) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"market", "code", "price", "open", "high", "low", "pre_close", "volume", "amount", "active"})
	for _, q := range items {
		_ = cw.Write([]string{
			tdx.MarketString(q.Market), q.Code,
			fmt.Sprintf("%.2f", q.Price), fmt.Sprintf("%.2f", q.Open),
			fmt.Sprintf("%.2f", q.High), fmt.Sprintf("%.2f", q.Low),
			fmt.Sprintf("%.2f", q.PreClose), strconv.FormatInt(q.Volume, 10),
			fmt.Sprintf("%.0f", q.Amount), strconv.FormatUint(uint64(q.Active), 10),
		})
	}
	cw.Flush()
}

// ---------------------------------------------------------------------------
// limit (涨跌停)
// ---------------------------------------------------------------------------

func runLimit(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("limit", flag.ExitOnError)
	board := fs.String("board", "6", "board code: 0=SH, 2=SZ, 6=A股, 7=B股, 8=科创板, 12=北证, 14=创业板")
	limitType := fs.String("type", "all", "filter: up|down|all")
	count := fs.Int("count", 500, "max stocks to scan")
	fs.Parse(args)

	sortOrder := tdx.SortDesc
	if *limitType == "down" {
		sortOrder = tdx.SortAsc
	}
	items, err := client.GetBoardMembers(*board, tdx.SortChangePct, *count, sortOrder)
	if err != nil {
		fatalf("get board members failed: %v", err)
	}

	var limitUp, limitDown []tdx.BoardMembersItem
	for _, item := range items {
		if item.BuyPriceLimit > 0 && item.Close >= item.BuyPriceLimit {
			limitUp = append(limitUp, item)
		}
		if item.SellPriceLimit > 0 && item.Close <= item.SellPriceLimit {
			limitDown = append(limitDown, item)
		}
	}

	switch strings.ToLower(format) {
	case "table", "":
		if *limitType == "all" || *limitType == "up" {
			renderLimitTable(w, "涨停", limitUp)
		}
		if *limitType == "all" || *limitType == "down" {
			renderLimitTable(w, "跌停", limitDown)
		}
	case "json":
		result := map[string][]tdx.BoardMembersItem{}
		if *limitType == "all" || *limitType == "up" {
			result["limit_up"] = limitUp
		}
		if *limitType == "all" || *limitType == "down" {
			result["limit_down"] = limitDown
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
	default:
		fatalf("limit: unsupported format: %s", format)
	}
}

func renderLimitTable(w *os.File, label string, items []tdx.BoardMembersItem) {
	fmt.Fprintf(w, "\n[%s] %d 只\n", label, len(items))
	fmt.Fprintf(w, "%-6s %-8s %-10s %10s %10s %10s %8s %10s %6s %6s %6s\n",
		"Mkt", "Code", "Name", "Close", "LimitUp", "LimitDn", "Chg%", "Amount", "UpDay", "Act", "Turn")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 100))
	for _, item := range items {
		changePct := calcChangePct(item.Close, item.PreClose)
		fmt.Fprintf(w, "%-6s %-8s %-10s %10.2f %10.2f %10.2f %7.2f%% %10.0f %6d %6d %5.1f%%\n",
			tdx.MarketString(item.Market), item.Code, item.Name,
			item.Close, item.BuyPriceLimit, item.SellPriceLimit, changePct,
			item.Amount, item.ConsecutiveUp, item.Activity, item.Turnover)
	}
}

func calcChangePct(close, preClose float32) float32 {
	if preClose == 0 {
		return 0
	}
	return (close - preClose) / preClose * 100
}

// ---------------------------------------------------------------------------
// board-members
// ---------------------------------------------------------------------------

func runBoardMembers(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("board-members", flag.ExitOnError)
	board := fs.String("board", "6", "board code: 0=SH, 2=SZ, 6=A股, 7=B股, 8=科创板, 12=北证, 14=创业板")
	sortStr := fs.String("sort", "change_pct", "sort: change_pct|code|amount|turnover")
	count := fs.Int("count", 20, "number of stocks")
	fs.Parse(args)

	sortType := tdx.SortChangePct
	switch *sortStr {
	case "code":
		sortType = tdx.SortCode
	case "amount":
		sortType = 0x07
	case "turnover":
		sortType = 0x1b
	}

	items, err := client.GetBoardMembers(*board, sortType, *count, tdx.SortDesc)
	if err != nil {
		fatalf("get board members failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderBoardMembersTable(w, items)
	case "json":
		renderBoardMembersJSON(w, items)
	default:
		fatalf("board-members: unsupported format: %s", format)
	}
}

func renderBoardMembersTable(w *os.File, items []tdx.BoardMembersItem) {
	fmt.Fprintf(w, "%-6s %-8s %-10s %8s %8s %8s %7s %8s %8s %8s %8s %7s %5s %5s %7s\n",
		"Mkt", "Code", "Name", "Close", "PrcCls", "Open", "Chg%", "Amount", "Turn%", "VolR", "Avg", "PE", "UpD", "Act", "5d%")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 120))
	for _, item := range items {
		changePct := calcChangePct(item.Close, item.PreClose)
		fmt.Fprintf(w, "%-6s %-8s %-10s %8.2f %8.2f %8.2f %6.2f%% %8.0f %7.1f%% %7.2f %8.2f %6.1f %4d %4d %6.1f%%\n",
			tdx.MarketString(item.Market), item.Code, item.Name,
			item.Close, item.PreClose, item.Open, changePct,
			item.Amount, item.Turnover, item.VolRatio, item.AvgPrice,
			item.PEDynamic, item.ConsecutiveUp, item.Activity, item.Change5d)
	}
}

func renderBoardMembersJSON(w *os.File, items []tdx.BoardMembersItem) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(items)
}

// ---------------------------------------------------------------------------
// lhb (龙虎榜)
// ---------------------------------------------------------------------------

func runLHB(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("lhb", flag.ExitOnError)
	code := fs.String("code", "", "stock code (e.g. 600000, sh600000)")
	fs.Parse(args)

	if *code == "" {
		fatalf("lhb: -code is required")
	}

	records, err := client.GetLHB(*code)
	if err != nil {
		fatalf("get lhb failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderLHBTable(w, records)
	case "json":
		renderLHBJSON(w, records)
	default:
		fatalf("lhb: unsupported format: %s", format)
	}
}

func renderLHBTable(w *os.File, records []tdx.LHBRecord) {
	if len(records) == 0 {
		fmt.Fprintln(w, "No LHB records found")
		return
	}
	for _, rec := range records {
		fmt.Fprintf(w, "\n[%s] %s  涨跌幅:%.2f%%  成交量:%.0f万股  成交额:%.0f万元\n",
			rec.Date, rec.InfoType, rec.ChangePct, rec.Volume, rec.Amount)

		fmt.Fprintf(w, "  %-40s %14s %14s\n", "买入营业部", "买入(万)", "卖出(万)")
		fmt.Fprintf(w, "  %s\n", strings.Repeat("-", 72))
		for _, s := range rec.BuySeats {
			fmt.Fprintf(w, "  %-40s %14.2f %14.2f\n", s.Name, s.BuyAmt, s.SellAmt)
		}

		fmt.Fprintf(w, "  %-40s %14s %14s\n", "卖出营业部", "买入(万)", "卖出(万)")
		fmt.Fprintf(w, "  %s\n", strings.Repeat("-", 72))
		for _, s := range rec.SellSeats {
			fmt.Fprintf(w, "  %-40s %14.2f %14.2f\n", s.Name, s.BuyAmt, s.SellAmt)
		}
	}
}

func renderLHBJSON(w *os.File, records []tdx.LHBRecord) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(records)
}

// ---------------------------------------------------------------------------
// ticks (batch tick)
// ---------------------------------------------------------------------------

func runTicks(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("ticks", flag.ExitOnError)
	codes := fs.String("codes", "", "comma-separated stock codes (e.g. sh600000,sz000001,sz300750)")
	fs.Parse(args)

	if *codes == "" {
		fatalf("ticks: -codes is required")
	}

	codeList := splitCodes(*codes)
	if len(codeList) == 0 {
		fatalf("ticks: no valid codes")
	}

	ticks, err := client.GetTicks(codeList)
	if err != nil {
		fatalf("get ticks failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderTicksTable(w, ticks)
	case "json":
		renderTicksJSON(w, ticks)
	default:
		fatalf("ticks: unsupported format: %s", format)
	}
}

func renderTicksTable(w *os.File, ticks []tdx.Tick) {
	fmt.Fprintf(w, "%-6s %-8s %10s %10s %10s %10s %12s %12s\n",
		"Mkt", "Code", "Price", "PrevClose", "Open", "High", "Volume", "Amount")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 90))
	for _, t := range ticks {
		fmt.Fprintf(w, "%-6s %-8s %10.2f %10.2f %10.2f %10.2f %12s %12s\n",
			tdx.MarketString(t.Market), t.Code, t.Price, t.PrevClose, t.Open, t.High,
			tdx.FormatVolume(t.Volume), tdx.FormatAmount(t.Amount))
		fmt.Fprintf(w, "  %s %8.2f%%  Bid1=%-8.2f(%-8s) Ask1=%-8.2f(%-8s)\n",
			t.Time, t.CalcChangeRate(),
			t.BuyLevels[0].Price, tdx.FormatVolume(t.BuyLevels[0].Volume),
			t.SellLevels[0].Price, tdx.FormatVolume(t.SellLevels[0].Volume))
	}
}

func renderTicksJSON(w *os.File, ticks []tdx.Tick) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(ticks)
}

// ---------------------------------------------------------------------------
// bq (batch quote, 0x054C)
// ---------------------------------------------------------------------------

func runBatchQuote(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("bq", flag.ExitOnError)
	codes := fs.String("codes", "", "comma-separated stock codes (e.g. sh600000,sz000001,sz300750)")
	fs.Parse(args)

	if *codes == "" {
		fatalf("bq: -codes is required")
	}

	codeList := splitCodes(*codes)
	if len(codeList) == 0 {
		fatalf("bq: no valid codes")
	}

	quotes, err := client.GetBatchQuotes(codeList)
	if err != nil {
		fatalf("get batch quotes failed: %v", err)
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderBatchQuoteTable(w, quotes)
	case "json":
		renderBatchQuoteJSON(w, quotes)
	default:
		fatalf("bq: unsupported format: %s", format)
	}
}

func renderBatchQuoteTable(w *os.File, quotes []tdx.BatchQuote) {
	fmt.Fprintf(w, "%-6s %-8s %8s %8s %8s %8s %8s %12s %8s %8s %7s %7s %7s\n",
		"Mkt", "Code", "Price", "Open", "High", "Low", "PreCls", "Amount", "Bid1", "Ask1", "Rise%", "ShtT%", "VolR")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 115))
	for _, q := range quotes {
		fmt.Fprintf(w, "%-6s %-8s %8.2f %8.2f %8.2f %8.2f %8.2f %12.0f %8.2f %8.2f %6.1f %6.1f %6.1f\n",
			tdx.MarketString(q.Market), q.Code, q.Price, q.Open, q.High, q.Low, q.PreClose,
			q.Amount, q.BidPrice, q.AskPrice, q.RiseSpeed, q.ShortTurnover, q.VolRatio)
	}
}

func renderBatchQuoteJSON(w *os.File, quotes []tdx.BatchQuote) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(quotes)
}

func splitCodes(s string) []string {
	var result []string
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			result = append(result, c)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// help
// ---------------------------------------------------------------------------

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: tdx-cli [global flags] <command> [command flags]\n\n")
	fmt.Fprintf(os.Stderr, "Global flags:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nCommands:\n%s", availableCommands())
}

func availableCommands() string {
	return `  probe             Probe server nodes (latency & reachability)
  kline             K-line data (candlestick bars)
  tick              Real-time quote snapshot (5-level order book)
  ticks             Batch tick snapshots (multiple stocks)
  tickchart         Intraday tick chart (price + volume)
  transaction       Intraday trade records
  history-trade     Historical intraday trade records
  codelist          Stock code list (paginated)
  finance           Financial fundamentals (F10)
  xdxr              Ex-rights/ex-dividend history
  company           F10 information section list
  company-content   F10 section content
  topboard          9-in-1 ranking boards (涨跌幅/振幅/换手率)
  quotes            Sorted quotes list (by any field)
  limit             Limit up/down stocks (涨跌停)
  board-members     Board member quotes (bitmap fields)
  lhb               Dragon-Tiger list (龙虎榜)
  bq                Batch compact quote (涨速/短换手/量比)

Examples:
  tdx-cli probe                    # probe main servers
  tdx-cli probe -sp                # probe SP servers
  tdx-cli kline -code sh600000 -period day -count 20
  tdx-cli tick -code sz000001
  tdx-cli tickchart -code sh600000 -count 240
  tdx-cli finance -code sh600000
  tdx-cli topboard -market 6 -size 10 -board increase
  tdx-cli quotes -market 6 -sort change_pct -count 10
  tdx-cli limit -board 6 -type up
  tdx-cli board-members -board 6 -count 10
  tdx-cli lhb -code 002471
  tdx-cli ticks -codes sh600000,sz000001,sz300750
  tdx-cli bq -codes sh600000,sz000001,sz300750
`
}

// ---------------------------------------------------------------------------
// probe
// ---------------------------------------------------------------------------

func runProbe(args []string) {
	sp := false
	for _, a := range args {
		if a == "-sp" {
			sp = true
		}
	}

	var results []tdx.HostProbeResult
	if sp {
		results = tdx.CachedSPResults(0)
	} else {
		results = tdx.CachedMainResults(0)
	}

	fmt.Printf("%-4s %-40s %-24s %8s  %s\n", "No", "Name", "Address", "Latency", "Status")
	fmt.Println(strings.Repeat("-", 90))
	reachable := 0
	for i, r := range results {
		status := "FAIL"
		latency := ""
		if r.Reachable {
			status = "OK"
			latency = r.Latency.Round(time.Millisecond).String()
			reachable++
		}
		name := r.Name
		if len(name) > 36 {
			name = name[:33] + "..."
		}
		fmt.Printf("%-4d %-40s %-24s %8s  %s\n", i+1, name, r.Address, latency, status)
	}
	fmt.Printf("\nReachable: %d / %d\n", reachable, len(results))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// Suppress unused import warning for time (used by Transaction.Time).
var _ = time.Local
