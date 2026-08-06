package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/millken/tdx"
)

// runICFQSCommand 分发 ICFQS (HTTP 云端数据) 子命令。无需 TCP 拨号。
func runICFQSCommand(cmd string, args []string, w *os.File, format string) {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	addr := fs.String("addr", "", "ICFQS host:port (auto-pick first built-in if empty)")
	timeout := fs.Duration("timeout", 10*time.Second, "HTTP timeout")
	// 允许 -format 被本 FlagSet 接受（实际输出格式由全局 format 参数决定）。
	fs.String("format", "", "output format (same as global -format)")
	_ = fs.Parse(args)
	rest := fs.Args()

	client := newICFQSClient(*addr, *timeout)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout+2*time.Second)
	defer cancel()

	switch cmd {
	case "icfqs-hot-topics":
		runICFQSRaw(client, ctx, w, format, func() (map[string]any, error) { return client.ICFQSHotTopicsRaw(ctx) })
	case "icfqs-top-topics":
		n := icfqsIntFlag(rest, 0, 10)
		runICFQSRaw(client, ctx, w, format, func() (map[string]any, error) { return client.ICFQSTopTopicsRaw(ctx, n) })
	case "icfqs-new-topics":
		runICFQSRaw(client, ctx, w, format, func() (map[string]any, error) { return client.ICFQSNewTopicsRaw(ctx) })
	case "icfqs-events":
		runICFQSRaw(client, ctx, w, format, func() (map[string]any, error) { return client.ICFQSEventsRaw(ctx) })
	case "icfqs-topic-list":
		category := icfqsStrFlag(rest, 0, "gn")
		setcode := icfqsStrFlag(rest, 1, "1")
		page := icfqsIntFlag(rest, 2, 1)
		runICFQSRaw(client, ctx, w, format, func() (map[string]any, error) {
			return client.ICFQSTopicListRaw(ctx, category, setcode, page)
		})
	case "icfqs-search-topics":
		keyword := icfqsStrFlag(rest, 0, "")
		runICFQSRaw(client, ctx, w, format, func() (map[string]any, error) {
			return client.ICFQSSearchTopicsRaw(ctx, keyword)
		})
	case "icfqs-topic-detail":
		code := icfqsStrFlag(rest, 0, "")
		setcode := icfqsStrFlag(rest, 1, "990002")
		runICFQSRaw(client, ctx, w, format, func() (map[string]any, error) {
			return client.ICFQSTopicDetailRaw(ctx, code, setcode)
		})
	case "icfqs-topic-stocks":
		code := icfqsStrFlag(rest, 0, "")
		setcode := icfqsStrFlag(rest, 1, "990002")
		page := icfqsIntFlag(rest, 2, 1)
		size := icfqsIntFlag(rest, 3, 20)
		runICFQSRaw(client, ctx, w, format, func() (map[string]any, error) {
			return client.ICFQSTopicStocksRaw(ctx, code, setcode, page, size)
		})
	case "icfqs-quotes-batch":
		codes := icfqsStrFlag(rest, 0, "")
		runICFQSRaw(client, ctx, w, format, func() (map[string]any, error) {
			return client.ICFQSQuotesBatchRaw(ctx, parseICFQSCodes(codes), nil)
		})
	case "icfqs-lhb-detail":
		symbol := icfqsStrFlag(rest, 0, "")
		start := icfqsStrFlag(rest, 1, "")
		end := icfqsStrFlag(rest, 2, "")
		runICFQSRaw(client, ctx, w, format, func() (map[string]any, error) {
			return client.ICFQSLHBDetailRaw(ctx, symbol, start, end)
		})
	case "icfqs-mrfp-latest-date":
		runICFQSRaw(client, ctx, w, format, func() (map[string]any, error) { return client.ICFQSMRFPLatestDateRaw(ctx) })
	default:
		fatalf("unknown icfqs command: %s", cmd)
	}
}

// runICFQSRaw 执行一个 ICFQS 调用并输出结果。
func runICFQSRaw(client *tdx.ICFQSClient, ctx context.Context, w *os.File, format string, fn func() (map[string]any, error)) {
	raw, err := fn()
	if err != nil {
		fatalf("icfqs failed: %v", err)
	}
	switch strings.ToLower(format) {
	case "json", "":
		b, _ := json.MarshalIndent(raw, "", "  ")
		fmt.Fprintln(w, string(b))
	case "table":
		printICFQSTables(w, raw)
	default:
		b, _ := json.MarshalIndent(raw, "", "  ")
		fmt.Fprintln(w, string(b))
	}
}

// printICFQSTables 以表格形式打印 ResultSets。
func printICFQSTables(w *os.File, raw map[string]any) {
	tables := tdx.ICFQSFormatTables(raw)
	if len(tables) == 0 {
		b, _ := json.MarshalIndent(raw, "", "  ")
		fmt.Fprintln(w, string(b))
		return
	}
	for ti, table := range tables {
		fmt.Fprintf(w, "=== Table %d (%d rows) ===\n", ti, len(table.Rows))
		if len(table.Rows) == 0 {
			continue
		}
		// 列顺序取首行 key。
		var cols []string
		for k := range table.Rows[0] {
			cols = append(cols, k)
		}
		fmt.Fprintln(w, strings.Join(cols, "\t"))
		for _, row := range table.Rows {
			vals := make([]string, 0, len(cols))
			for _, c := range cols {
				vals = append(vals, fmt.Sprint(row[c]))
			}
			fmt.Fprintln(w, strings.Join(vals, "\t"))
		}
	}
}

func newICFQSClient(addr string, timeout time.Duration) *tdx.ICFQSClient {
	opts := []tdx.ICFQSOption{tdx.WithICFQSTimeout(timeout)}
	if addr != "" {
		opts = append(opts, tdx.WithICFQSAddress(addr))
	} else if addrs := tdx.ICFQSAddresses(); len(addrs) > 0 {
		opts = append(opts, tdx.WithICFQSAddress(addrs[0]))
	}
	return tdx.NewICFQS(opts...)
}

// parseICFQSCodes 解析 "sh600000,sz000001" 为 ICFQSCode 列表。
func parseICFQSCodes(s string) []tdx.ICFQSCode {
	var out []tdx.ICFQSCode
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(strings.ToLower(c))
		if c == "" {
			continue
		}
		setcode := "1"
		code := c
		if strings.HasPrefix(c, "sh") {
			setcode = "1"
			code = c[2:]
		} else if strings.HasPrefix(c, "sz") {
			setcode = "0"
			code = c[2:]
		} else if strings.HasPrefix(c, "0") || strings.HasPrefix(c, "3") {
			setcode = "0"
		}
		out = append(out, tdx.ICFQSCode{Setcode: setcode, Code: code})
	}
	return out
}

// icfqsStrFlag 从位置参数取字符串，缺省返回 def。
func icfqsStrFlag(args []string, idx int, def string) string {
	if idx < len(args) && args[idx] != "" {
		return args[idx]
	}
	return def
}

// icfqsIntFlag 从位置参数取整数，缺省返回 def。
func icfqsIntFlag(args []string, idx int, def int) int {
	if idx < len(args) && args[idx] != "" {
		var n int
		if _, err := fmt.Sscanf(args[idx], "%d", &n); err == nil {
			return n
		}
	}
	return def
}
