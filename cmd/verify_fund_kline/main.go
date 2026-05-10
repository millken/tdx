package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/millken/tdx"
)

func main() {
	code := flag.String("code", "009708", "fund code")
	period := flag.String("period", "day", "kline period")
	count := flag.Int("count", 5, "number of bars to verify")
	exHost := flag.String("ex-host", "", "7727 extension quote host:port, auto-detect if empty")
	legacyFundHost := flag.String("fund-host", "", "deprecated: use -ex-host")
	mainHost := flag.String("main-host", "", "7709 main host:port, auto-detect if empty")
	timeout := flag.Duration("timeout", 8*time.Second, "dial/request timeout")
	flag.Parse()

	if *count <= 0 {
		failf("-count must be > 0")
	}

	selectedExHost := *exHost
	if selectedExHost == "" {
		selectedExHost = *legacyFundHost
	}

	exClient, err := dialEx(selectedExHost, *timeout)
	if err != nil {
		failf("dial ex client failed: %v", err)
	}
	defer exClient.Close()

	exKlines, err := exClient.GetKline(*code, *period, *count)
	if err != nil {
		failf("GetFundKline failed: %v", err)
	}
	if len(exKlines) == 0 {
		failf("GetFundKline returned 0 bars")
	}

	mainClient, err := dialMain(*mainHost, *timeout)
	if err != nil {
		failf("dial main client failed: %v", err)
	}
	defer mainClient.Close()

	autoKlines, err := mainClient.GetKline(*code, *period, *count)
	if err != nil {
		failf("GetKline failed: %v", err)
	}
	if len(autoKlines) == 0 {
		failf("GetKline returned 0 bars")
	}

	if err := compareKlines(exKlines, autoKlines); err != nil {
		failf("verification failed: %v", err)
	}

	printSample("GetFundKline", exKlines)
	printSample("GetKline", autoKlines)
	fmt.Printf("verification passed for code=%s period=%s count=%d\n", *code, *period, len(exKlines))
}

func dialEx(host string, timeout time.Duration) (*tdx.ExClient, error) {
	if host != "" {
		return tdx.DialEx(host, tdx.WithTimeout(timeout))
	}
	return tdx.DialExBest(nil, tdx.WithTimeout(timeout))
}

func dialMain(host string, timeout time.Duration) (*tdx.MainClient, error) {
	if host != "" {
		return tdx.Dial(host, tdx.WithTimeout(timeout))
	}
	return tdx.DialBest(nil, tdx.WithTimeout(timeout))
}

func compareKlines(fundKlines, autoKlines []tdx.Kline) error {
	if len(fundKlines) != len(autoKlines) {
		return fmt.Errorf("bar count mismatch: fund=%d auto=%d", len(fundKlines), len(autoKlines))
	}
	for i := range fundKlines {
		if !fundKlines[i].Time.Equal(autoKlines[i].Time) {
			return fmt.Errorf("bar %d date mismatch: fund=%s auto=%s", i, fundKlines[i].Time.Format("2006-01-02 15:04:05"), autoKlines[i].Time.Format("2006-01-02 15:04:05"))
		}
		if !closeEnough(fundKlines[i].Open, autoKlines[i].Open) ||
			!closeEnough(fundKlines[i].High, autoKlines[i].High) ||
			!closeEnough(fundKlines[i].Low, autoKlines[i].Low) ||
			!closeEnough(fundKlines[i].Close, autoKlines[i].Close) ||
			!closeEnough(fundKlines[i].Amount, autoKlines[i].Amount) ||
			fundKlines[i].Volume != autoKlines[i].Volume {
			return fmt.Errorf("bar %d payload mismatch: fund=%+v auto=%+v", i, fundKlines[i], autoKlines[i])
		}
	}
	return nil
}

func closeEnough(a, b float64) bool {
	return math.Abs(a-b) <= 1e-6
}

func printSample(tag string, klines []tdx.Kline) {
	first := klines[0]
	last := klines[len(klines)-1]
	fmt.Printf("%s count=%d first=%s %.4f last=%s %.4f\n",
		tag,
		len(klines),
		first.Time.Format("2006-01-02"), first.Close,
		last.Time.Format("2006-01-02"), last.Close,
	)
}

func failf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
