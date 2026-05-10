package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
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
	case "hot-board":
		runHotBoardCommand(*host, *spHost, args[1:], w, *format)
		return
	case "board-heatmap":
		runBoardHeatmapCommand(*host, *spHost, args[1:], w, *format)
		return
	}

	boardCmd := cmd == "limit" || cmd == "board-members"
	if boardCmd {
		client, err := dialBoardClient(*spHost)
		if err != nil {
			fatalf("dial board host failed: %v", err)
		}
		defer client.Close()
		runCommand(cmd, client, args[1:], w, *format)
		return
	}

	// Build pool or single client
	if *poolSize > 0 {
		var pool *tdx.Pool
		var err error
		pool, err = tdx.NewPool(*poolSize)
		if err != nil {
			fatalf("create pool failed: %v", err)
		}
		defer pool.Close()

		client := pool.Get()
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
	case "board-heatmap":
		fatalf("board-heatmap should be dispatched before runCommand")
	default:
		fatalf("unknown command: %s\n\n%s", cmd, availableCommands())
	}
}

func runHotBoardCommand(host, spHost string, args []string, w *os.File, format string) {
	quotesClient, err := dialMainClient(host)
	if err != nil {
		fatalf("dial host failed: %v", err)
	}
	defer quotesClient.Close()

	boardClient, err := dialBoardClient(spHost)
	if err != nil {
		fatalf("dial board host failed: %v", err)
	}
	defer boardClient.Close()

	runHotBoard(quotesClient, boardClient, args, w, format)
}

func runBoardHeatmapCommand(host, spHost string, args []string, w *os.File, format string) {
	quotesClient, err := dialMainClient(host)
	if err != nil {
		fatalf("dial host failed: %v", err)
	}
	defer quotesClient.Close()

	boardClient, err := dialBoardClient(spHost)
	if err != nil {
		fatalf("dial board host failed: %v", err)
	}
	defer boardClient.Close()

	runBoardHeatmap(quotesClient, boardClient, args, w, format)
}

func dialMainClient(host string) (*tdx.MainClient, error) {
	if host != "" {
		return tdx.Dial(host)
	}
	return tdx.DialBest(nil)
}

func dialBoardClient(host string) (*tdx.MainClient, error) {
	if host != "" {
		return tdx.Dial(host)
	}
	return tdx.DialBest(tdx.BestSPAddresses(0))
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

type hotBoardSnapshot struct {
	Market    uint16  `json:"market"`
	Code      string  `json:"code"`
	Name      string  `json:"name,omitempty"`
	Heat      float64 `json:"heat"`
	Price     float64 `json:"price"`
	PreClose  float64 `json:"pre_close"`
	ChangePct float64 `json:"change_pct"`
	Change3d  float64 `json:"change_3d"`
	Amount    float64 `json:"amount"`
	RiseSpeed float64 `json:"rise_speed"`
	Active    uint16  `json:"active"`
}

type hotStockSnapshot struct {
	Market        uint16  `json:"market"`
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Heat          float64 `json:"heat"`
	Close         float32 `json:"close"`
	PreClose      float32 `json:"pre_close"`
	ChangePct     float32 `json:"change_pct"`
	MainNetAmount float32 `json:"main_net,omitempty"`
	Amount        float32 `json:"amount"`
	Turnover      float32 `json:"turnover"`
	VolRatio      float32 `json:"vol_ratio"`
	Activity      uint32  `json:"activity"`
	ConsecutiveUp int32   `json:"consecutive_up"`
}

type hotBoardEntry struct {
	Board       hotBoardSnapshot   `json:"board"`
	Members     []hotStockSnapshot `json:"members,omitempty"`
	MemberError string             `json:"member_error,omitempty"`
}

type hotBoardOutput struct {
	Category   string          `json:"category"`
	Sort       string          `json:"sort"`
	BoardCount int             `json:"board_count"`
	StockSort  string          `json:"stock_sort"`
	StockCount int             `json:"stock_count"`
	Boards     []hotBoardEntry `json:"boards"`
}

// ---------------------------------------------------------------------------
// hot-board
// ---------------------------------------------------------------------------

func runHotBoard(quotesClient, boardClient *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("hot-board", flag.ExitOnError)
	categoryStr := fs.String("category", "gn", "board category: hy|hy2|gn|fg|dq")
	boardCount := fs.Int("board-count", 10, "number of hot boards to fetch")
	stockCount := fs.Int("stock-count", 3, "number of hot stocks per board")
	boardSortStr := fs.String("sort", "change_pct", "board sort: change_pct|change_3d|main_net|amount|activity|speed_pct|price|code")
	stockSortStr := fs.String("stock-sort", "change_pct", "member sort: change_pct|amount|turnover|activity|vol_ratio")
	fs.Parse(args)

	if *boardCount <= 0 {
		fatalf("hot-board: -board-count must be > 0")
	}
	if *stockCount <= 0 {
		fatalf("hot-board: -stock-count must be > 0")
	}

	category, categoryLabel, err := parseHotBoardCategory(*categoryStr)
	if err != nil {
		fatalf("hot-board: %v", err)
	}
	boardSort, err := parseHotBoardQuoteSort(*boardSortStr)
	if err != nil {
		fatalf("hot-board: %v", err)
	}
	stockSort, err := parseHotBoardMemberSort(*stockSortStr)
	if err != nil {
		fatalf("hot-board: %v", err)
	}

	boards, err := quotesClient.GetQuotesList(category, boardSort, 0, *boardCount, false)
	if err != nil {
		fatalf("hot-board: get board quotes failed: %v", err)
	}
	boardNames := resolveHotBoardNames(quotesClient, boards)

	result := hotBoardOutput{
		Category:   categoryLabel,
		Sort:       *boardSortStr,
		BoardCount: *boardCount,
		StockSort:  *stockSortStr,
		StockCount: *stockCount,
		Boards:     make([]hotBoardEntry, 0, len(boards)),
	}

	for _, board := range boards {
		entry := hotBoardEntry{Board: makeHotBoardSnapshot(board, lookupHotBoardName(boardNames, board.Market, board.Code))}

		members, memberErr := boardClient.GetBoardMembers(board.Code, stockSort, *stockCount, tdx.SortDesc)
		if memberErr != nil {
			entry.MemberError = memberErr.Error()
			result.Boards = append(result.Boards, entry)
			continue
		}

		entry.Members = make([]hotStockSnapshot, 0, len(members))
		for _, member := range members {
			entry.Members = append(entry.Members, makeHotStockSnapshot(member))
		}
		assignHotStockHeat(entry.Members)
		result.Boards = append(result.Boards, entry)
	}
	enrichHotBoardEntries(quotesClient, result.Boards)
	if isChange3dSort(*boardSortStr) {
		sort.SliceStable(result.Boards, func(i, j int) bool {
			return result.Boards[i].Board.Change3d > result.Boards[j].Board.Change3d
		})
	}
	assignHotBoardHeat(result.Boards)

	switch strings.ToLower(format) {
	case "table", "":
		renderHotBoardTable(w, result)
	case "json":
		renderHotBoardJSON(w, result)
	default:
		fatalf("hot-board: unsupported format: %s", format)
	}
}

func resolveHotBoardNames(client *tdx.Client, items []tdx.QuotesItem) map[uint16]map[string]string {
	names := make(map[uint16]map[string]string)
	targets := make(map[uint16]map[string]struct{})

	for _, item := range items {
		if item.Code == "" {
			continue
		}
		if targets[item.Market] == nil {
			targets[item.Market] = make(map[string]struct{})
		}
		targets[item.Market][item.Code] = struct{}{}
	}

	for market, codes := range targets {
		resolved := resolveHotBoardMarketNames(client, market, codes)
		if len(resolved) > 0 {
			names[market] = resolved
		}
	}

	return names
}

func resolveHotBoardMarketNames(client *tdx.Client, market uint16, targetCodes map[string]struct{}) map[string]string {
	resolved := make(map[string]string)
	if len(targetCodes) == 0 {
		return resolved
	}

	for start := uint16(0); len(resolved) < len(targetCodes); {
		page, err := client.GetMarketCodes(market, start)
		if err != nil || page == nil || len(page.List) == 0 {
			break
		}

		for _, item := range page.List {
			if _, ok := targetCodes[item.Code]; ok {
				resolved[item.Code] = item.Name
			}
		}

		if page.Count == 0 || int(page.Count) < 1000 {
			break
		}
		start += page.Count
	}

	return resolved
}

func lookupHotBoardName(names map[uint16]map[string]string, market uint16, code string) string {
	if byMarket := names[market]; byMarket != nil {
		return byMarket[code]
	}
	return ""
}

func parseHotBoardCategory(s string) (uint16, string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "hy":
		return tdx.CategoryBoardHY, "hy", nil
	case "hy2":
		return tdx.CategoryBoardHY2, "hy2", nil
	case "gn":
		return tdx.CategoryBoardGN, "gn", nil
	case "fg":
		return tdx.CategoryBoardFG, "fg", nil
	case "dq":
		return tdx.CategoryBoardDQ, "dq", nil
	default:
		return 0, "", fmt.Errorf("unknown category %q (valid: hy|hy2|gn|fg|dq)", s)
	}
}

func parseHotBoardQuoteSort(s string) (uint16, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "change_pct":
		return tdx.SortChangePct, nil
	case "change_3d", "strength":
		return tdx.SortChange3dPct, nil
	case "main_net":
		return tdx.SortMainNetAmount, nil
	case "amount":
		return tdx.SortAmount, nil
	case "activity":
		return tdx.SortActivity, nil
	case "speed_pct":
		return tdx.SortSpeedPct, nil
	case "price":
		return tdx.SortPrice, nil
	case "code":
		return tdx.SortCode, nil
	default:
		return 0, fmt.Errorf("unknown sort %q (valid: change_pct|change_3d|main_net|amount|activity|speed_pct|price|code)", s)
	}
}

func parseHotBoardMemberSort(s string) (uint16, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "change_pct":
		return tdx.BoardMembersSortChangePct, nil
	case "amount":
		return tdx.BoardMembersSortAmount, nil
	case "turnover":
		return tdx.BoardMembersSortTurnover, nil
	case "activity":
		return tdx.SortActivity, nil
	case "vol_ratio":
		return tdx.BoardMembersSortVolRatio, nil
	case "main_net":
		return tdx.BoardMembersSortMainNetAmount, nil
	default:
		return 0, fmt.Errorf("unknown stock sort %q (valid: change_pct|amount|turnover|activity|vol_ratio|main_net)", s)
	}
}

func makeHotBoardSnapshot(item tdx.QuotesItem, name string) hotBoardSnapshot {
	return hotBoardSnapshot{
		Market:    item.Market,
		Code:      item.Code,
		Name:      name,
		Price:     item.Price,
		PreClose:  item.PreClose,
		ChangePct: calcQuoteChangePct(item.Price, item.PreClose),
		Amount:    item.Amount,
		RiseSpeed: item.RiseSpeed,
		Active:    item.Active,
	}
}

func makeHotStockSnapshot(item tdx.BoardMembersItem) hotStockSnapshot {
	return hotStockSnapshot{
		Market:        item.Market,
		Code:          item.Code,
		Name:          item.Name,
		Close:         item.Close,
		PreClose:      item.PreClose,
		ChangePct:     calcChangePct(item.Close, item.PreClose),
		MainNetAmount: item.MainNetAmount,
		Amount:        item.Amount,
		Turnover:      item.Turnover,
		VolRatio:      item.VolRatio,
		Activity:      item.Activity,
		ConsecutiveUp: item.ConsecutiveUp,
	}
}

func assignHotBoardHeat(entries []hotBoardEntry) {
	if len(entries) == 0 {
		return
	}

	changeScores := rankMetricScores(len(entries), func(i int) float64 { return entries[i].Board.ChangePct })
	amountScores := rankMetricScores(len(entries), func(i int) float64 { return entries[i].Board.Amount })
	speedScores := rankMetricScores(len(entries), func(i int) float64 { return entries[i].Board.RiseSpeed })
	activeScores := rankMetricScores(len(entries), func(i int) float64 { return float64(entries[i].Board.Active) })

	for i := range entries {
		entries[i].Board.Heat = roundHeatScore(
			0.45*changeScores[i] +
				0.25*amountScores[i] +
				0.15*speedScores[i] +
				0.15*activeScores[i],
		)
	}
}

func assignHotStockHeat(items []hotStockSnapshot) {
	if len(items) == 0 {
		return
	}

	changeScores := rankMetricScores(len(items), func(i int) float64 { return float64(items[i].ChangePct) })
	amountScores := rankMetricScores(len(items), func(i int) float64 { return float64(items[i].Amount) })
	turnoverScores := rankMetricScores(len(items), func(i int) float64 { return float64(items[i].Turnover) })
	volRatioScores := rankMetricScores(len(items), func(i int) float64 { return float64(items[i].VolRatio) })
	upScores := rankMetricScores(len(items), func(i int) float64 { return float64(items[i].ConsecutiveUp) })

	for i := range items {
		items[i].Heat = roundHeatScore(
			0.45*changeScores[i] +
				0.20*amountScores[i] +
				0.20*turnoverScores[i] +
				0.10*volRatioScores[i] +
				0.05*upScores[i],
		)
	}
}

func rankMetricScores(count int, value func(i int) float64) []float64 {
	scores := make([]float64, count)
	if count == 0 {
		return scores
	}
	if count == 1 {
		scores[0] = 100
		return scores
	}

	indexes := make([]int, count)
	for i := range indexes {
		indexes[i] = i
	}

	sort.SliceStable(indexes, func(i, j int) bool {
		return value(indexes[i]) > value(indexes[j])
	})

	for pos := 0; pos < count; {
		end := pos + 1
		current := value(indexes[pos])
		for end < count && value(indexes[end]) == current {
			end++
		}

		avgRank := float64(pos+end-1) / 2
		percentile := 100 * (float64(count-1) - avgRank) / float64(count-1)
		for _, idx := range indexes[pos:end] {
			scores[idx] = percentile
		}
		pos = end
	}

	return scores
}

func roundHeatScore(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

func calcQuoteChangePct(price, preClose float64) float64 {
	if preClose == 0 {
		return 0
	}
	return (price - preClose) / preClose * 100
}

func renderHotBoardTable(w *os.File, result hotBoardOutput) {
	fmt.Fprintf(w, "category=%s sort=%s boards=%d stock_sort=%s stock_count=%d\n\n",
		result.Category, result.Sort, result.BoardCount, result.StockSort, result.StockCount)

	for idx, entry := range result.Boards {
		fmt.Fprintf(w, "[%02d] %-8s %-12s %-3s heat=%5.1f chg=%6.2f%% chg3d=%6.2f%% amount=%10s speed=%6.2f%% active=%d\n",
			idx+1,
			entry.Board.Code,
			entry.Board.Name,
			tdx.MarketString(entry.Board.Market),
			entry.Board.Heat,
			entry.Board.ChangePct,
			entry.Board.Change3d,
			tdx.FormatAmount(entry.Board.Amount),
			entry.Board.RiseSpeed,
			entry.Board.Active,
		)

		if entry.MemberError != "" {
			fmt.Fprintf(w, "     members: %s\n\n", entry.MemberError)
			continue
		}

		fmt.Fprintf(w, "     %-6s %-8s %-10s %6s %8s %8s %10s %8s %8s %5s %5s\n",
			"Mkt", "Code", "Name", "Heat", "Close", "Chg%", "MainNet", "Amount", "Turn%", "UpD", "Act")
		fmt.Fprintf(w, "     %s\n", strings.Repeat("-", 90))
		for _, member := range entry.Members {
			fmt.Fprintf(w, "     %-6s %-8s %-10s %6.1f %8.2f %7.2f%% %10s %8s %7.1f%% %5d %5d\n",
				tdx.MarketString(member.Market),
				member.Code,
				member.Name,
				member.Heat,
				member.Close,
				member.ChangePct,
				tdx.FormatAmount(float64(member.MainNetAmount)),
				tdx.FormatAmount(float64(member.Amount)),
				member.Turnover,
				member.ConsecutiveUp,
				member.Activity,
			)
		}
		fmt.Fprintln(w)
	}
}

func renderHotBoardJSON(w *os.File, result hotBoardOutput) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}

type boardHeatmapBoard struct {
	Market        uint16  `json:"market"`
	Code          string  `json:"code"`
	Name          string  `json:"name,omitempty"`
	Price         float64 `json:"price"`
	PreClose      float64 `json:"pre_close"`
	ChangePct     float64 `json:"change_pct"`
	Change3d      float64 `json:"change_3d"`
	MainNetAmount float64 `json:"main_net,omitempty"`
	Amount        float64 `json:"amount"`
	Volume        int64   `json:"volume"`
	CurVolume     int64   `json:"cur_volume"`
	ServerTime    string  `json:"server_time,omitempty"`
	RiseSpeed     float64 `json:"rise_speed"`
	ShortTurnover float32 `json:"short_turnover"`
	Min2Amount    float32 `json:"amount_2m"`
	VolRatio      float32 `json:"vol_ratio"`
	Depth         float32 `json:"depth"`
	Active        uint16  `json:"active"`
}

type boardHeatmapMember struct {
	Market        uint16  `json:"market"`
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Close         float32 `json:"close"`
	PreClose      float32 `json:"pre_close"`
	ChangePct     float32 `json:"change_pct"`
	MainNetAmount float32 `json:"main_net,omitempty"`
	Amount        float32 `json:"amount"`
	Turnover      float32 `json:"turnover"`
	VolRatio      float32 `json:"vol_ratio"`
	Activity      uint32  `json:"activity"`
	ConsecutiveUp int32   `json:"consecutive_up"`
}

type boardHeatmapOutput struct {
	Category      string               `json:"category"`
	Sort          string               `json:"sort"`
	Count         int                  `json:"count"`
	SelectedCode  string               `json:"selected_code"`
	MemberSort    string               `json:"member_sort"`
	MemberCount   int                  `json:"member_count"`
	SelectedBoard boardHeatmapBoard    `json:"selected_board"`
	Boards        []boardHeatmapBoard  `json:"boards"`
	Members       []boardHeatmapMember `json:"members"`
}

// ---------------------------------------------------------------------------
// board-heatmap
// ---------------------------------------------------------------------------

func runBoardHeatmap(quotesClient, boardClient *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("board-heatmap", flag.ExitOnError)
	categoryStr := fs.String("category", "hy", "board category: hy|hy2|gn|fg|dq")
	count := fs.Int("count", 40, "number of boards to fetch")
	boardSortStr := fs.String("sort", "change_pct", "board sort: change_pct|change_3d|main_net|amount|activity|speed_pct|price|code")
	selectedCode := fs.String("board", "", "selected board code (default: first board in result)")
	memberCount := fs.Int("member-count", 40, "number of member stocks to fetch for the selected board")
	memberSortStr := fs.String("member-sort", "change_pct", "member sort: change_pct|amount|turnover|activity|vol_ratio")
	fs.Parse(args)

	if *count <= 0 {
		fatalf("board-heatmap: -count must be > 0")
	}
	if *memberCount <= 0 {
		fatalf("board-heatmap: -member-count must be > 0")
	}

	category, categoryLabel, err := parseHotBoardCategory(*categoryStr)
	if err != nil {
		fatalf("board-heatmap: %v", err)
	}
	boardSort, err := parseHotBoardQuoteSort(*boardSortStr)
	if err != nil {
		fatalf("board-heatmap: %v", err)
	}
	memberSort, err := parseHotBoardMemberSort(*memberSortStr)
	if err != nil {
		fatalf("board-heatmap: %v", err)
	}

	boards, err := quotesClient.GetQuotesList(category, boardSort, 0, *count, false)
	if err != nil {
		fatalf("board-heatmap: get board quotes failed: %v", err)
	}
	if len(boards) == 0 {
		fatalf("board-heatmap: empty board list")
	}

	boardNames := resolveHotBoardNames(quotesClient, boards)
	boardItems := make([]boardHeatmapBoard, 0, len(boards))
	for _, board := range boards {
		boardItems = append(boardItems, makeBoardHeatmapBoard(board, lookupHotBoardName(boardNames, board.Market, board.Code)))
	}
	enrichBoardHeatmapBoards(quotesClient, boardItems)
	if isChange3dSort(*boardSortStr) {
		sort.SliceStable(boardItems, func(i, j int) bool {
			return boardItems[i].Change3d > boardItems[j].Change3d
		})
	}

	selected := *selectedCode
	if selected == "" {
		selected = boardItems[0].Code
	}

	selectedBoard, ok := findBoardHeatmapBoard(boardItems, selected)
	if !ok {
		selectedBoard, err = fetchBoardHeatmapBoard(quotesClient, selected)
		if err != nil {
			fatalf("board-heatmap: selected board %q not found in current board list and fallback quote failed: %v", selected, err)
		}
	}
	selectedBoard.MainNetAmount = fetchBoardMainNetAmount(boardClient, selected)
	for i := range boardItems {
		if boardItems[i].Code == selectedBoard.Code {
			boardItems[i].MainNetAmount = selectedBoard.MainNetAmount
			break
		}
	}

	members, err := boardClient.GetBoardMembers(selected, memberSort, *memberCount, tdx.SortDesc)
	if err != nil {
		fatalf("board-heatmap: get board members failed: %v", err)
	}

	memberItems := make([]boardHeatmapMember, 0, len(members))
	for _, member := range members {
		memberItems = append(memberItems, makeBoardHeatmapMember(member))
	}

	result := boardHeatmapOutput{
		Category:      categoryLabel,
		Sort:          *boardSortStr,
		Count:         *count,
		SelectedCode:  selected,
		MemberSort:    *memberSortStr,
		MemberCount:   *memberCount,
		SelectedBoard: selectedBoard,
		Boards:        boardItems,
		Members:       memberItems,
	}

	switch strings.ToLower(format) {
	case "table", "":
		renderBoardHeatmapTable(w, result)
	case "json":
		renderBoardHeatmapJSON(w, result)
	default:
		fatalf("board-heatmap: unsupported format: %s", format)
	}
}

func makeBoardHeatmapBoard(item tdx.QuotesItem, name string) boardHeatmapBoard {
	return boardHeatmapBoard{
		Market:        item.Market,
		Code:          item.Code,
		Name:          name,
		Price:         item.Price,
		PreClose:      item.PreClose,
		ChangePct:     calcQuoteChangePct(item.Price, item.PreClose),
		Amount:        item.Amount,
		Volume:        item.Volume,
		CurVolume:     item.CurVol,
		ServerTime:    item.ServerTime,
		RiseSpeed:     item.RiseSpeed,
		ShortTurnover: item.ShortTurnover,
		Min2Amount:    item.Min2Amount,
		VolRatio:      item.VolRatio,
		Depth:         item.Depth,
		Active:        item.Active,
	}
}

func makeBoardHeatmapMember(item tdx.BoardMembersItem) boardHeatmapMember {
	return boardHeatmapMember{
		Market:        item.Market,
		Code:          item.Code,
		Name:          item.Name,
		Close:         item.Close,
		PreClose:      item.PreClose,
		ChangePct:     calcChangePct(item.Close, item.PreClose),
		MainNetAmount: item.MainNetAmount,
		Amount:        item.Amount,
		Turnover:      item.Turnover,
		VolRatio:      item.VolRatio,
		Activity:      item.Activity,
		ConsecutiveUp: item.ConsecutiveUp,
	}
}

func findBoardHeatmapBoard(items []boardHeatmapBoard, code string) (boardHeatmapBoard, bool) {
	for _, item := range items {
		if item.Code == code {
			return item, true
		}
	}
	return boardHeatmapBoard{}, false
}

func fetchBoardHeatmapBoard(client *tdx.Client, code string) (boardHeatmapBoard, error) {
	quotes, err := client.GetBatchQuotes([]string{normalizeBoardTickCode(code)})
	if err == nil && len(quotes) > 0 {
		q := quotes[0]
		name, _ := lookupBoardNameByCode(client, q.Market, q.Code)
		item := boardHeatmapBoard{
			Market:        q.Market,
			Code:          q.Code,
			Name:          name,
			Price:         q.Price,
			PreClose:      q.PreClose,
			ChangePct:     calcQuoteChangePct(q.Price, q.PreClose),
			Amount:        float64(q.Amount),
			Volume:        q.Volume,
			CurVolume:     q.CurVolume,
			ServerTime:    q.ServerTime,
			RiseSpeed:     float64(q.RiseSpeed),
			ShortTurnover: q.ShortTurnover,
			Min2Amount:    q.Min2Amount,
			VolRatio:      q.VolRatio,
			Depth:         q.Depth,
			Active:        q.Active,
		}
		item.Change3d = fetchBoardChange3dPct(client, item.Code)
		return item, nil
	}

	tick, err := client.GetTick(normalizeBoardTickCode(code))
	if err != nil {
		return boardHeatmapBoard{}, err
	}

	name, _ := lookupBoardNameByCode(client, tick.Market, tick.Code)
	item := boardHeatmapBoard{
		Market:    tick.Market,
		Code:      tick.Code,
		Name:      name,
		Price:     tick.Price,
		PreClose:  tick.PrevClose,
		ChangePct: tick.CalcChangeRate(),
		Amount:    tick.Amount,
		Volume:    tick.TotalVolume,
		CurVolume: tick.Volume,
		RiseSpeed: 0,
		Active:    tick.Active1,
	}
	item.Change3d = fetchBoardChange3dPct(client, item.Code)
	return item, nil
}

func enrichBoardHeatmapBoards(client *tdx.Client, items []boardHeatmapBoard) {
	if len(items) == 0 {
		return
	}

	codes := make([]string, 0, len(items))
	indexByCode := make(map[string]int, len(items))
	for i, item := range items {
		codes = append(codes, normalizeBoardTickCode(item.Code))
		indexByCode[item.Code] = i
	}

	quotes, err := client.GetBatchQuotes(codes)
	if err != nil {
		return
	}

	for _, q := range quotes {
		idx, ok := indexByCode[q.Code]
		if !ok {
			continue
		}
		items[idx].Price = q.Price
		items[idx].PreClose = q.PreClose
		items[idx].ChangePct = calcQuoteChangePct(q.Price, q.PreClose)
		items[idx].Amount = float64(q.Amount)
		items[idx].Volume = q.Volume
		items[idx].CurVolume = q.CurVolume
		items[idx].ServerTime = q.ServerTime
		items[idx].RiseSpeed = float64(q.RiseSpeed)
		items[idx].ShortTurnover = q.ShortTurnover
		items[idx].Min2Amount = q.Min2Amount
		items[idx].VolRatio = q.VolRatio
		items[idx].Depth = q.Depth
		items[idx].Active = q.Active
	}

	for i := range items {
		items[i].Change3d = fetchBoardChange3dPct(client, items[i].Code)
	}
}

func enrichHotBoardEntries(client *tdx.Client, entries []hotBoardEntry) {
	for i := range entries {
		entries[i].Board.Change3d = fetchBoardChange3dPct(client, entries[i].Board.Code)
	}
}

func fetchBoardChange3dPct(client *tdx.Client, code string) float64 {
	klines, err := client.GetKline(normalizeBoardTickCode(code), tdx.PeriodDay, 4)
	if err != nil {
		return 0
	}
	return calcKlineWindowChangePct(klines, 3)
}

func fetchBoardMainNetAmount(client *tdx.Client, code string) float64 {
	items, err := client.GetBoardMembers(code, tdx.BoardMembersSortCode, 2000, tdx.SortDesc)
	if err != nil {
		return 0
	}
	var total float64
	for _, item := range items {
		total += float64(item.MainNetAmount)
	}
	return total
}

func calcKlineWindowChangePct(klines []tdx.Kline, window int) float64 {
	if window <= 0 || len(klines) < window+1 {
		return 0
	}
	base := klines[len(klines)-window-1].Close
	if base == 0 {
		return 0
	}
	last := klines[len(klines)-1].Close
	return (last - base) / base * 100
}

func isChange3dSort(sortName string) bool {
	switch strings.ToLower(strings.TrimSpace(sortName)) {
	case "change_3d", "strength":
		return true
	default:
		return false
	}
}

func lookupBoardNameByCode(client *tdx.Client, market uint16, code string) (string, bool) {
	for start := uint16(0); ; {
		page, err := client.GetMarketCodes(market, start)
		if err != nil || page == nil || len(page.List) == 0 {
			return "", false
		}

		for _, item := range page.List {
			if item.Code == code {
				return item.Name, true
			}
		}

		if page.Count == 0 || int(page.Count) < 1000 {
			return "", false
		}
		start += page.Count
	}
}

func normalizeBoardTickCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if len(code) == 8 && (strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") || strings.HasPrefix(code, "bj")) {
		return code
	}
	if strings.HasPrefix(code, "399") {
		return "sz" + code
	}
	if strings.HasPrefix(code, "899") {
		return "bj" + code
	}
	return "sh" + code
}

func renderBoardHeatmapTable(w *os.File, result boardHeatmapOutput) {
	fmt.Fprintf(w, "category=%s sort=%s count=%d selected=%s member_sort=%s member_count=%d\n\n",
		result.Category, result.Sort, result.Count, result.SelectedCode, result.MemberSort, result.MemberCount)

	fmt.Fprintf(w, "[Boards]\n")
	fmt.Fprintf(w, "%-8s %-12s %-4s %8s %8s %10s %12s %8s %7s %8s\n", "Code", "Name", "Mkt", "Chg%", "3d%", "MainNet", "Amount", "Speed", "ShtT%", "VolR")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 82))
	for _, board := range result.Boards {
		fmt.Fprintf(w, "%-8s %-12s %-4s %7.2f%% %7.2f%% %10s %12s %7.2f%% %6.2f%% %8.2f\n",
			board.Code,
			board.Name,
			tdx.MarketString(board.Market),
			board.ChangePct,
			board.Change3d,
			tdx.FormatAmount(board.MainNetAmount),
			tdx.FormatAmount(board.Amount),
			board.RiseSpeed,
			board.ShortTurnover,
			board.VolRatio,
		)
	}

	fmt.Fprintf(w, "\n[Selected Board] %s %s\n", result.SelectedBoard.Code, result.SelectedBoard.Name)
	fmt.Fprintf(w, "chg=%0.2f%% chg3d=%0.2f%% main_net=%s amount=%s speed=%0.2f%% short_turn=%0.2f%% vol_ratio=%0.2f amount_2m=%s active=%d time=%s\n\n",
		result.SelectedBoard.ChangePct,
		result.SelectedBoard.Change3d,
		tdx.FormatAmount(result.SelectedBoard.MainNetAmount),
		tdx.FormatAmount(result.SelectedBoard.Amount),
		result.SelectedBoard.RiseSpeed,
		result.SelectedBoard.ShortTurnover,
		result.SelectedBoard.VolRatio,
		tdx.FormatAmount(float64(result.SelectedBoard.Min2Amount)),
		result.SelectedBoard.Active,
		result.SelectedBoard.ServerTime,
	)

	fmt.Fprintf(w, "[Members]\n")
	fmt.Fprintf(w, "%-6s %-8s %-10s %8s %8s %10s %8s %8s %5s %5s\n",
		"Mkt", "Code", "Name", "Close", "Chg%", "MainNet", "Amount", "Turn%", "UpD", "Act")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 82))
	for _, member := range result.Members {
		fmt.Fprintf(w, "%-6s %-8s %-10s %8.2f %7.2f%% %10s %8s %7.1f%% %5d %5d\n",
			tdx.MarketString(member.Market),
			member.Code,
			member.Name,
			member.Close,
			member.ChangePct,
			tdx.FormatAmount(float64(member.MainNetAmount)),
			tdx.FormatAmount(float64(member.Amount)),
			member.Turnover,
			member.ConsecutiveUp,
			member.Activity,
		)
	}
}

func renderBoardHeatmapJSON(w *os.File, result boardHeatmapOutput) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}

// ---------------------------------------------------------------------------
// quotes
// ---------------------------------------------------------------------------

func runQuotes(client *tdx.Client, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet("quotes", flag.ExitOnError)
	market := fs.Int("market", 6, "market: 0=SZ, 1=SH, 2=BJ, 6=A股, 7=B股, 8=科创板, 12=北证, 14=创业板")
	sortStr := fs.String("sort", "change_pct", "sort field: change_pct|change_3d|amplitude_pct|turnover_rate|vol_ratio|speed_pct|code|price|volume|amount|pe|entrust|inout|locked_ratio|locked_amount|float_mcap|total_mcap|strength|activity|short_turnover|vol_speed|main_net|amount_2m")
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
	case "change_3d", "strength":
		sortType = tdx.SortChange3dPct
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
	sortStr := fs.String("sort", "change_pct", "sort: change_pct|code|amount|turnover|vol_ratio|main_net")
	sortTypeRaw := fs.Int("sort-type", -1, "raw sort type override for 0x122C protocol")
	count := fs.Int("count", 20, "number of stocks")
	fs.Parse(args)

	sortType := tdx.BoardMembersSortChangePct
	if *sortTypeRaw >= 0 {
		sortType = uint16(*sortTypeRaw)
	} else {
		var err error
		sortType, err = parseBoardMembersSort(*sortStr)
		if err != nil {
			fatalf("board-members: %v", err)
		}
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

func parseBoardMembersSort(s string) (uint16, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "change_pct":
		return tdx.BoardMembersSortChangePct, nil
	case "code":
		return tdx.BoardMembersSortCode, nil
	case "amount":
		return tdx.BoardMembersSortAmount, nil
	case "turnover":
		return tdx.BoardMembersSortTurnover, nil
	case "vol_ratio":
		return tdx.BoardMembersSortVolRatio, nil
	case "main_net":
		return tdx.BoardMembersSortMainNetAmount, nil
	default:
		return 0, fmt.Errorf("unknown sort %q (valid: change_pct|code|amount|turnover|vol_ratio|main_net)", s)
	}
}

func renderBoardMembersTable(w *os.File, items []tdx.BoardMembersItem) {
	fmt.Fprintf(w, "%-6s %-8s %-10s %8s %8s %8s %7s %10s %8s %8s %8s %8s %7s %5s %5s %7s\n",
		"Mkt", "Code", "Name", "Close", "PrcCls", "Open", "Chg%", "MainNet", "Amount", "Turn%", "VolR", "Avg", "PE", "UpD", "Act", "5d%")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 120))
	for _, item := range items {
		changePct := calcChangePct(item.Close, item.PreClose)
		fmt.Fprintf(w, "%-6s %-8s %-10s %8.2f %8.2f %8.2f %6.2f%% %10s %8.0f %7.1f%% %7.2f %8.2f %6.1f %4d %4d %6.1f%%\n",
			tdx.MarketString(item.Market), item.Code, item.Name,
			item.Close, item.PreClose, item.Open, changePct,
			tdx.FormatAmount(float64(item.MainNetAmount)), item.Amount, item.Turnover, item.VolRatio, item.AvgPrice,
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
	board-heatmap     Board heatmap dataset (板块列表 + 选中板块成分股)
	hot-board         Hot boards with top member stocks (板块排行 + 个股)
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
	tdx-cli -format json board-heatmap -category hy -count 40 -board 881314 -member-count 40
	tdx-cli hot-board -category gn -board-count 10 -stock-count 3
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
