package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"
)

type quotesListProbeRow struct {
	Code          string
	Head          [9]int64
	Amount        float32
	Mid           [8]int64
	ServerTimeRaw int64
	Tail          [56]byte
}

type quotesListCandidateScore struct {
	Label      string
	Violations int
	First      float64
	Last       float64
}

func TestProbeQuotesListUnknownTail(t *testing.T) {
	if os.Getenv("TDX_LIVE") == "" {
		t.Skip("set TDX_LIVE=1 to run live quotes list probe")
	}

	client, err := DialBest(nil)
	if err != nil {
		t.Fatalf("DialBest() error = %v", err)
	}
	defer client.Close()

	count := 24
	if s := os.Getenv("TDX_PROBE_COUNT"); s != "" {
		if n, convErr := strconv.Atoi(s); convErr == nil && n > 2 {
			count = n
		}
	}

	category := CategoryBoardHY
	if s := os.Getenv("TDX_PROBE_CATEGORY"); s != "" {
		if n, convErr := strconv.Atoi(s); convErr == nil {
			category = uint16(n)
		}
	}

	sorts := []struct {
		name string
		typ  uint16
	}{
		{name: "short_turnover", typ: SortShortTurnover},
		{name: "amount_2m", typ: SortAmount2M},
		{name: "vol_ratio", typ: SortVolRatio},
		{name: "strength", typ: SortStrengthPct},
		{name: "vol_speed", typ: SortVolSpeedPct},
		{name: "main_net", typ: SortMainNetAmount},
	}

	for _, tc := range sorts {
		rows, err := fetchQuotesListProbeRows(client, category, tc.typ, count)
		if err != nil {
			t.Fatalf("fetchQuotesListProbeRows(%s) error = %v", tc.name, err)
		}
		if len(rows) < 3 {
			t.Fatalf("fetchQuotesListProbeRows(%s) len = %d, want >= 3", tc.name, len(rows))
		}

		scores := scoreQuotesListTailCandidates(rows)
		t.Logf("sort=%s category=%d rows=%d", tc.name, category, len(rows))
		for i := 0; i < len(scores) && i < 8; i++ {
			s := scores[i]
			t.Logf("  candidate=%-14s violations=%d first=%g last=%g", s.Label, s.Violations, s.First, s.Last)
		}
		for i := 0; i < len(rows) && i < 3; i++ {
			t.Logf("  row[%d] code=%s server_time_raw=%d tail30_53=% x", i, rows[i].Code, rows[i].ServerTimeRaw, rows[i].Tail[30:54])
		}
	}

	t.Logf("probe completed at %s", time.Now().Format(time.RFC3339))
}

func fetchQuotesListProbeRows(c *MainClient, category uint16, sortType uint16, count int) ([]quotesListProbeRow, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	c.drainPending()

	packet, err := RequestQuotesListFrame(0x000A0401, 0x01, category, sortType, 0, uint16(count), SortDesc, 0)
	if err != nil {
		return nil, err
	}
	if err := c.sendRaw(packet); err != nil {
		return nil, err
	}

	response, err := c.waitForCMD(DirectFrameTypeQuotesList, 8*time.Second)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty quotes list response")
	}

	return decodeQuotesListProbeRows(response.Body.Decoded)
}

func decodeQuotesListProbeRows(body []byte) ([]quotesListProbeRow, error) {
	if len(body) < 4 {
		return nil, fmt.Errorf("quotes list body too short: %d", len(body))
	}

	count := int(binary.LittleEndian.Uint16(body[2:4]))
	body = body[4:]

	rows := make([]quotesListProbeRow, 0, count)
	for i := 0; i < count; i++ {
		if len(body) < 9 {
			break
		}

		code := trimNulASCII(body[1:7])
		body = body[9:]

		var err error
		var head [9]int64
		var serverTimeRaw int64
		for j := 0; j < 9; j++ {
			var value int64
			body, value, err = cutPrice(body)
			if err != nil {
				return nil, fmt.Errorf("quotes list probe record %d head field %d: %w", i, j, err)
			}
			head[j] = value
			if j == 5 {
				serverTimeRaw = value
			}
		}

		if len(body) < 4 {
			return nil, fmt.Errorf("quotes list probe record %d amount: unexpected EOF", i)
		}
		amount := math.Float32frombits(binary.LittleEndian.Uint32(body[:4]))
		body = body[4:]

		var mid [8]int64
		for j := 0; j < 8; j++ {
			body, mid[j], err = cutPrice(body)
			if err != nil {
				return nil, fmt.Errorf("quotes list probe record %d mid field %d: %w", i, j, err)
			}
		}

		if len(body) < 56 {
			return nil, fmt.Errorf("quotes list probe record %d tail: unexpected EOF", i)
		}

		var tail [56]byte
		copy(tail[:], body[:56])
		rows = append(rows, quotesListProbeRow{
			Code:          code,
			Head:          head,
			Amount:        amount,
			Mid:           mid,
			ServerTimeRaw: serverTimeRaw,
			Tail:          tail,
		})
		body = body[56:]
	}

	return rows, nil
}

func scoreQuotesListTailCandidates(rows []quotesListProbeRow) []quotesListCandidateScore {
	type reader struct {
		label string
		read  func(quotesListProbeRow) float64
	}

	readers := []reader{
		{label: "amount", read: func(r quotesListProbeRow) float64 { return float64(r.Amount) }},
		{label: "head[0]", read: func(r quotesListProbeRow) float64 { return float64(r.Head[0]) }},
		{label: "head[1]", read: func(r quotesListProbeRow) float64 { return float64(r.Head[1]) }},
		{label: "head[2]", read: func(r quotesListProbeRow) float64 { return float64(r.Head[2]) }},
		{label: "head[3]", read: func(r quotesListProbeRow) float64 { return float64(r.Head[3]) }},
		{label: "head[4]", read: func(r quotesListProbeRow) float64 { return float64(r.Head[4]) }},
		{label: "head[5]", read: func(r quotesListProbeRow) float64 { return float64(r.Head[5]) }},
		{label: "head[6]", read: func(r quotesListProbeRow) float64 { return float64(r.Head[6]) }},
		{label: "head[7]", read: func(r quotesListProbeRow) float64 { return float64(r.Head[7]) }},
		{label: "head[8]", read: func(r quotesListProbeRow) float64 { return float64(r.Head[8]) }},
		{label: "mid[0]", read: func(r quotesListProbeRow) float64 { return float64(r.Mid[0]) }},
		{label: "mid[1]", read: func(r quotesListProbeRow) float64 { return float64(r.Mid[1]) }},
		{label: "mid[2]", read: func(r quotesListProbeRow) float64 { return float64(r.Mid[2]) }},
		{label: "mid[3]", read: func(r quotesListProbeRow) float64 { return float64(r.Mid[3]) }},
		{label: "mid[4]", read: func(r quotesListProbeRow) float64 { return float64(r.Mid[4]) }},
		{label: "mid[5]", read: func(r quotesListProbeRow) float64 { return float64(r.Mid[5]) }},
		{label: "mid[6]", read: func(r quotesListProbeRow) float64 { return float64(r.Mid[6]) }},
		{label: "mid[7]", read: func(r quotesListProbeRow) float64 { return float64(r.Mid[7]) }},
		{label: "i16@2", read: func(r quotesListProbeRow) float64 {
			return float64(int16(binary.LittleEndian.Uint16(r.Tail[2:4]))) / 100
		}},
		{label: "i16@4", read: func(r quotesListProbeRow) float64 {
			return float64(int16(binary.LittleEndian.Uint16(r.Tail[4:6]))) / 100
		}},
		{label: "f32@6", read: func(r quotesListProbeRow) float64 {
			return float64(math.Float32frombits(binary.LittleEndian.Uint32(r.Tail[6:10])))
		}},
		{label: "f32@22", read: func(r quotesListProbeRow) float64 {
			return float64(math.Float32frombits(binary.LittleEndian.Uint32(r.Tail[22:26])))
		}},
		{label: "f32@26", read: func(r quotesListProbeRow) float64 {
			return float64(math.Float32frombits(binary.LittleEndian.Uint32(r.Tail[26:30])))
		}},
	}

	for off := 30; off <= 50; off += 4 {
		off := off
		readers = append(readers,
			reader{label: fmt.Sprintf("f32@%d", off), read: func(r quotesListProbeRow) float64 {
				return float64(math.Float32frombits(binary.LittleEndian.Uint32(r.Tail[off : off+4])))
			}},
			reader{label: fmt.Sprintf("i32@%d", off), read: func(r quotesListProbeRow) float64 {
				return float64(int32(binary.LittleEndian.Uint32(r.Tail[off : off+4])))
			}},
		)
	}
	for off := 30; off <= 52; off += 2 {
		off := off
		readers = append(readers,
			reader{label: fmt.Sprintf("i16@%d", off), read: func(r quotesListProbeRow) float64 {
				return float64(int16(binary.LittleEndian.Uint16(r.Tail[off : off+2])))
			}},
		)
	}

	scores := make([]quotesListCandidateScore, 0, len(readers))
	for _, r := range readers {
		violations := 0
		first := r.read(rows[0])
		last := r.read(rows[len(rows)-1])
		allEqual := true
		for i := 0; i+1 < len(rows); i++ {
			left := r.read(rows[i])
			right := r.read(rows[i+1])
			if isInvalidProbeValue(left) || isInvalidProbeValue(right) {
				violations += 1000
				continue
			}
			if left != right {
				allEqual = false
			}
			if left < right {
				violations++
			}
		}
		if allEqual {
			violations += 10000
		}
		scores = append(scores, quotesListCandidateScore{
			Label:      r.label,
			Violations: violations,
			First:      first,
			Last:       last,
		})
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Violations != scores[j].Violations {
			return scores[i].Violations < scores[j].Violations
		}
		return scores[i].Label < scores[j].Label
	})

	return scores
}

func isInvalidProbeValue(v float64) bool {
	return math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e20
}
