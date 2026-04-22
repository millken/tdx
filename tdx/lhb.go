package tdx

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// LHBRecord represents one entry in the Dragon-Tiger list.
type LHBRecord struct {
	Date        string
	InfoType    string
	ChangePct   float64
	Volume      float64 // 万股
	Amount      float64 // 万元
	BuySeats    []LHBSeat
	SellSeats   []LHBSeat
}

// LHBSeat represents one seat (broker branch) in the Dragon-Tiger list.
type LHBSeat struct {
	Name   string
	BuyAmt float64 // 万元
	SellAmt float64 // 万元
}

var (
	reRecord   = regexp.MustCompile(`●交易日期:(\d{4}-\d{2}-\d{2})\s+信息类型:(.+)`)
	reSummary  = regexp.MustCompile(`涨跌幅\(%\):([\-\d.]+)\s+成交量\(万股\):([\d.]+)\s+成交额\(万元\):([\d.]+)`)
	reSeatData = regexp.MustCompile(`│(.+)│\s*([\-\d.]+)│\s*([\-\d.]+)│`)
)

// ParseLHB extracts Dragon-Tiger list records from the "资金动向" F10 section text.
func ParseLHB(text string) []LHBRecord {
	var records []LHBRecord
	lines := strings.Split(text, "\n")

	var cur *LHBRecord
	var section string // "buy" or "sell"
	var pendingName string

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")

		// New record
		if m := reRecord.FindStringSubmatch(line); m != nil {
			if cur != nil {
				records = append(records, *cur)
			}
			cur = &LHBRecord{Date: m[1], InfoType: strings.TrimSpace(m[2])}
			section = ""
			pendingName = ""
			continue
		}

		if cur == nil {
			continue
		}

		// Summary line
		if m := reSummary.FindStringSubmatch(line); m != nil {
			cur.ChangePct, _ = strconv.ParseFloat(m[1], 64)
			cur.Volume, _ = strconv.ParseFloat(m[2], 64)
			cur.Amount, _ = strconv.ParseFloat(m[3], 64)
			continue
		}

		// Section header
		if strings.Contains(line, "买入前五") {
			section = "buy"
			pendingName = ""
			continue
		}
		if strings.Contains(line, "卖出前五") {
			section = "sell"
			pendingName = ""
			continue
		}

		// Skip box-drawing, header rows
		if strings.Contains(line, "营业部名称") || strings.Contains(line, "金额") ||
			strings.Contains(line, "────") || strings.Contains(line, "┌") ||
			strings.Contains(line, "└") || strings.Contains(line, "├") {
			continue
		}

		// Data row: │name│buy│sell│
		if strings.Contains(line, "│") {
			m := reSeatData.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			name := strings.TrimSpace(m[1])
			buyAmt, _ := strconv.ParseFloat(strings.TrimSpace(m[2]), 64)
			sellAmt, _ := strconv.ParseFloat(strings.TrimSpace(m[3]), 64)

			// Check if this is a continuation line (name is empty or just whitespace)
			if name == "" || name == "                    " {
				if pendingName != "" && (buyAmt != 0 || sellAmt != 0) {
					pendingName += name
					seat := LHBSeat{Name: strings.TrimSpace(pendingName), BuyAmt: buyAmt, SellAmt: sellAmt}
					if section == "buy" {
						cur.BuySeats = append(cur.BuySeats, seat)
					} else if section == "sell" {
						cur.SellSeats = append(cur.SellSeats, seat)
					}
					pendingName = ""
				} else if pendingName != "" {
					pendingName += name
				}
				continue
			}

			// If we had a pending name without amounts, this is a new row
			if pendingName != "" {
				pendingName = ""
			}

			// Check if this row has amounts (not a pure continuation)
			if buyAmt != 0 || sellAmt != 0 {
				seat := LHBSeat{Name: name, BuyAmt: buyAmt, SellAmt: sellAmt}
				if section == "buy" {
					cur.BuySeats = append(cur.BuySeats, seat)
				} else if section == "sell" {
					cur.SellSeats = append(cur.SellSeats, seat)
				}
			} else {
				pendingName = name
			}
		}
	}

	if cur != nil {
		records = append(records, *cur)
	}

	return records
}

// GetLHB retrieves the Dragon-Tiger list for the given stock code.
// It finds the "资金动向" section in F10 data, downloads it, and parses LHB records.
func (c *Client) GetLHB(code string) ([]LHBRecord, error) {
	categories, err := c.GetCompanyCategory(code)
	if err != nil {
		return nil, fmt.Errorf("get company category: %w", err)
	}

	var filename string
	var start, length uint32
	for _, cat := range categories {
		if cat.Name == "资金动向" {
			filename = cat.Filename
			start = cat.Start
			length = cat.Length
			break
		}
	}
	if filename == "" {
		return nil, fmt.Errorf("资金动向 section not found")
	}

	content, err := c.GetCompanyContent(code, filename, start, length)
	if err != nil {
		return nil, fmt.Errorf("get company content: %w", err)
	}

	// Extract only the 【1.交易龙虎榜】 part (skip the TOC line)
	idx := strings.Index(content, "\n【1.交易龙虎榜】")
	if idx < 0 {
		idx = strings.Index(content, "【1.交易龙虎榜】\n")
		if idx < 0 {
			return ParseLHB(content), nil
		}
		idx++ // skip past the 【1. marker to start at newline
	}
	content = content[idx+1:] // skip the newline

	return ParseLHB(content), nil
}
