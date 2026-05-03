package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/millken/tdx"
)

func main() {
	code := flag.String("code", "009708", "fund code")
	mode := flag.Uint("mode", 50, "2488 request mode")
	exHost := flag.String("ex-host", "", "7727 extension quote host:port, auto-detect if empty")
	timeout := flag.Duration("timeout", 8*time.Second, "dial/request timeout")
	flag.Parse()

	exClient, err := dialEx(*exHost, *timeout)
	if err != nil {
		failf("dial ex client failed: %v", err)
	}
	defer exClient.Close()

	exDetail, err := exClient.GetFundDetailMode(*code, uint16(*mode))
	if err != nil {
		failf("ex GetFundDetail failed: %v", err)
	}

	printSample("ex", exDetail)
	fmt.Printf("verification passed for code=%s mode=%d items=%d\n", *code, *mode, len(exDetail.Items))
}

func dialEx(host string, timeout time.Duration) (*tdx.ExClient, error) {
	if host != "" {
		return tdx.DialEx(host, tdx.WithTimeout(timeout))
	}
	return tdx.DialExBest(nil, tdx.WithTimeout(timeout))
}

func printSample(tag string, detail *tdx.FundDetail) {
	if len(detail.Items) == 0 {
		fmt.Printf("%s category=%d code=%s items=0\n", tag, detail.Category, detail.Code)
		return
	}
	first := detail.Items[0]
	fmt.Printf("%s category=%d code=%s items=%d first_id=%d first_values=%v\n",
		tag,
		detail.Category,
		detail.Code,
		len(detail.Items),
		first.ID,
		first.Values,
	)
}

func failf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
