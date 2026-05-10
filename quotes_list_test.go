package tdx

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestDecodeQuotesListTailFields(t *testing.T) {
	body := make([]byte, 0, 128)
	head := make([]byte, 4)
	binary.LittleEndian.PutUint16(head[2:4], 1)
	body = append(body, head...)

	record := make([]byte, 0, 96)
	record = append(record, byte(MarketShanghai))
	record = append(record, []byte("881314")...)
	record = binary.LittleEndian.AppendUint16(record, 4747)

	record = append(record, encodeSignedVarint(122507)...)
	record = append(record, encodeSignedVarint(-7041)...)
	record = append(record, encodeSignedVarint(-1500)...)
	record = append(record, encodeSignedVarint(2500)...)
	record = append(record, encodeSignedVarint(-3200)...)
	record = append(record, encodeSignedVarint(14524200)...)
	record = append(record, encodeSignedVarint(0)...)
	record = append(record, encodeSignedVarint(2163689)...)
	record = append(record, encodeSignedVarint(5799)...)

	amountBits := math.Float32bits(9175176192)
	record = binary.LittleEndian.AppendUint32(record, amountBits)

	record = append(record, encodeSignedVarint(1012345)...)
	record = append(record, encodeSignedVarint(1151344)...)
	record = append(record, encodeSignedVarint(0)...)
	record = append(record, encodeSignedVarint(0)...)
	record = append(record, encodeSignedVarint(-3)...)
	record = append(record, encodeSignedVarint(4)...)
	record = append(record, encodeSignedVarint(120)...)
	record = append(record, encodeSignedVarint(240)...)

	tail := make([]byte, 56)
	binary.LittleEndian.PutUint16(tail[2:4], uint16(int16(14)))
	binary.LittleEndian.PutUint16(tail[4:6], uint16(int16(408)))
	binary.LittleEndian.PutUint32(tail[6:10], math.Float32bits(74265600))
	binary.LittleEndian.PutUint32(tail[22:26], math.Float32bits(0.98016864))
	binary.LittleEndian.PutUint32(tail[26:30], math.Float32bits(1.25))
	record = append(record, tail...)
	body = append(body, record...)

	items, err := DecodeQuotesList(body)
	if err != nil {
		t.Fatalf("DecodeQuotesList() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("DecodeQuotesList() len = %d, want 1", len(items))
	}

	got := items[0]
	if got.Code != "881314" {
		t.Fatalf("code = %q, want 881314", got.Code)
	}
	if got.ServerTime != formatQuoteTime(14524200) {
		t.Fatalf("server_time = %q, want %q", got.ServerTime, formatQuoteTime(14524200))
	}
	if got.CurVol != 5799 {
		t.Fatalf("cur_vol = %d, want 5799", got.CurVol)
	}
	if got.InVol != 1012345 {
		t.Fatalf("in_vol = %d, want 1012345", got.InVol)
	}
	if got.OutVol != 1151344 {
		t.Fatalf("out_vol = %d, want 1151344", got.OutVol)
	}
	if got.RiseSpeed != 0.14 {
		t.Fatalf("rise_speed = %v, want 0.14", got.RiseSpeed)
	}
	if got.ShortTurnover != 4.08 {
		t.Fatalf("short_turnover = %v, want 4.08", got.ShortTurnover)
	}
	if got.Min2Amount != 74265600 {
		t.Fatalf("amount_2m = %v, want 74265600", got.Min2Amount)
	}
	if got.VolRatio != float32(0.98016864) {
		t.Fatalf("vol_ratio = %v, want 0.98016864", got.VolRatio)
	}
	if got.Depth != 1.25 {
		t.Fatalf("depth = %v, want 1.25", got.Depth)
	}
	if got.Active != 4747 {
		t.Fatalf("active = %d, want 4747", got.Active)
	}
}

func TestFormatQuoteTime(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{input: 0, want: "00:00:00"},
		{input: 14524200, want: "14:52:42"},
		{input: 9301500, want: "09:30:15"},
		{input: 152918546, want: "15:29:18.546"},
		{input: 14998037, want: "14:59:52.933"},
	}

	for _, tt := range tests {
		if got := formatQuoteTime(tt.input); got != tt.want {
			t.Fatalf("formatQuoteTime(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func encodeSignedVarint(value int64) []byte {
	negative := value < 0
	if negative {
		value = -value
	}

	first := byte(value & 0x3f)
	value >>= 6
	if negative {
		first |= 0x40
	}
	if value != 0 {
		first |= 0x80
	}

	buf := []byte{first}
	for value != 0 {
		b := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			b |= 0x80
		}
		buf = append(buf, b)
	}
	return buf
}
