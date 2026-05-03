// Package tdx provides a minimal Go client for the TDX (通达信) 7709 market data protocol.
//
// # Quick Start
//
//	c, err := tdx.Dial("110.41.147.114:7709")
//	if err != nil { ... }
//	defer c.Close()
//
//	klines, err := c.GetKline("sh600000", "day", 10)
//	if err != nil { ... }
//	for _, k := range klines {
//	    fmt.Printf("%s O=%.3f H=%.3f L=%.3f C=%.3f V=%d\n",
//	        k.Time.Format("2006-01-02"), k.Open, k.High, k.Low, k.Close, k.Volume)
//	}
package tdx
