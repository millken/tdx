package tdx

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func TestPeriodMap(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"day", false},
		{"week", false},
		{"month", false},
		{"1m", false},
		{"5m", false},
		{"15m", false},
		{"30m", false},
		{"60m", false},
		{"quarter", false},
		{"year", false},
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		_, ok := periodMap[tt.input]
		if ok == tt.wantErr {
			t.Errorf("periodMap[%q] = %v, want err=%v", tt.input, ok, tt.wantErr)
		}
	}
}

func TestKlineString(t *testing.T) {
	k := Kline{
		Time:   time.Date(2026, 4, 21, 15, 0, 0, 0, time.Local),
		Open:   10.05,
		High:   10.13,
		Low:    10.03,
		Close:  10.11,
		Volume: 43000232,
		Amount: 433931936,
	}
	s := k.String()
	if s == "" {
		t.Error("Kline.String() returned empty string")
	}
}

func TestFormatVolume(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{100, "100"},
		{10000, "1.00万"},
		{100000000, "1.00亿"},
		{123456789, "1.23亿"},
	}
	for _, tt := range tests {
		got := FormatVolume(tt.input)
		if got != tt.want {
			t.Errorf("FormatVolume(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{100.0, "100.00"},
		{10000.0, "1.00万"},
		{100000000.0, "1.00亿"},
	}
	for _, tt := range tests {
		got := FormatAmount(tt.input)
		if got != tt.want {
			t.Errorf("FormatAmount(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRequestFundKLineFrame(t *testing.T) {
	raw, err := RequestFundKLineFrame("009708", KlinePeriodDay, 1, 0, 420)
	if err != nil {
		t.Fatalf("RequestFundKLineFrame error: %v", err)
	}
	if len(raw) != 64 {
		t.Fatalf("RequestFundKLineFrame len = %d, want 64", len(raw))
	}
	if raw[0] != 0x01 {
		t.Fatalf("head = 0x%02x, want 0x01", raw[0])
	}
	if got := binary.LittleEndian.Uint16(raw[10:12]); got != FundKlineCMD {
		t.Fatalf("cmd = 0x%04x, want 0x%04x", got, FundKlineCMD)
	}
	body := raw[12:]
	if body[0] != 0x21 {
		t.Fatalf("category = 0x%02x, want 0x21", body[0])
	}
	if got := string(body[1:7]); got != "009708" {
		t.Fatalf("code = %q, want 009708", got)
	}
	if got := binary.LittleEndian.Uint16(body[24:26]); got != KlinePeriodDay {
		t.Fatalf("period = %d, want %d", got, KlinePeriodDay)
	}
	if got := binary.LittleEndian.Uint16(body[26:28]); got != 1 {
		t.Fatalf("times = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(body[28:32]); got != 0 {
		t.Fatalf("start = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(body[32:36]); got != 420 {
		t.Fatalf("count = %d, want 420", got)
	}
}

func TestRequestFundKLineFrameMonetaryFund(t *testing.T) {
	raw, err := RequestFundKLineFrame("730103", KlinePeriodDay, 1, 0, 5)
	if err != nil {
		t.Fatalf("RequestFundKLineFrame error: %v", err)
	}
	body := raw[12:]
	if body[0] != 0x22 {
		t.Fatalf("category = 0x%02x, want 0x22", body[0])
	}
	if got := string(body[1:7]); got != "730103" {
		t.Fatalf("code = %q, want 730103", got)
	}
}

func TestRequestFundDetailFrame(t *testing.T) {
	raw, err := RequestFundDetailFrame("009708", 0)
	if err != nil {
		t.Fatalf("RequestFundDetailFrame error: %v", err)
	}
	if len(raw) != 50 {
		t.Fatalf("RequestFundDetailFrame len = %d, want 50", len(raw))
	}
	if raw[0] != 0x01 {
		t.Fatalf("head = 0x%02x, want 0x01", raw[0])
	}
	if got := binary.LittleEndian.Uint16(raw[10:12]); got != FundDetailCMD {
		t.Fatalf("cmd = 0x%04x, want 0x%04x", got, FundDetailCMD)
	}
	body := raw[12:]
	if body[0] != 0x21 {
		t.Fatalf("category = 0x%02x, want 0x21", body[0])
	}
	if got := string(body[1:7]); got != "009708" {
		t.Fatalf("code = %q, want 009708", got)
	}
	if got := binary.LittleEndian.Uint16(body[28:30]); got != 50 {
		t.Fatalf("mode = %d, want 50", got)
	}
}

func TestRequestFundDetailFrameMonetaryFund(t *testing.T) {
	raw, err := RequestFundDetailFrame("730103", 0)
	if err != nil {
		t.Fatalf("RequestFundDetailFrame error: %v", err)
	}
	body := raw[12:]
	if body[0] != 0x22 {
		t.Fatalf("category = 0x%02x, want 0x22", body[0])
	}
}

func TestDecodeFundDetail(t *testing.T) {
	body := make([]byte, 38+2*16)
	body[0] = 0x22
	copy(body[1:36], []byte("730103"))
	binary.LittleEndian.PutUint16(body[36:38], 2)

	binary.LittleEndian.PutUint32(body[38:42], 7)
	for i, value := range []uint16{12610, 7, 30, 0, 0, 0} {
		binary.LittleEndian.PutUint16(body[42+i*2:44+i*2], value)
	}
	binary.LittleEndian.PutUint32(body[54:58], 11)
	for i, value := range []uint16{15, 20, 25, 30, 35, 40} {
		binary.LittleEndian.PutUint16(body[58+i*2:60+i*2], value)
	}

	detail, err := DecodeFundDetail(body)
	if err != nil {
		t.Fatalf("DecodeFundDetail error: %v", err)
	}
	if detail.Category != 0x22 {
		t.Fatalf("category = 0x%02x, want 0x22", detail.Category)
	}
	if detail.Code != "730103" {
		t.Fatalf("code = %q, want 730103", detail.Code)
	}
	if len(detail.Items) != 2 {
		t.Fatalf("items len = %d, want 2", len(detail.Items))
	}
	if detail.Items[0].ID != 7 || detail.Items[0].Values[0] != 12610 {
		t.Fatalf("first item = %+v", detail.Items[0])
	}
	item, ok := detail.FindItem(11)
	if !ok {
		t.Fatal("FindItem(11) = false, want true")
	}
	if item.Values[5] != 40 {
		t.Fatalf("last value = %d, want 40", item.Values[5])
	}
}

func TestDecodeFundKlines(t *testing.T) {
	body := make([]byte, 42+2*32)
	body[0] = 0x21
	copy(body[1:24], []byte("009708"))
	binary.LittleEndian.PutUint16(body[24:26], KlinePeriodDay)
	binary.LittleEndian.PutUint16(body[26:28], 1)
	binary.LittleEndian.PutUint16(body[40:42], 2)

	putRecord := func(offset int, date uint32, open, high, low, close, amount float32, volume uint32) {
		binary.LittleEndian.PutUint32(body[offset:offset+4], date)
		binary.LittleEndian.PutUint32(body[offset+4:offset+8], math.Float32bits(open))
		binary.LittleEndian.PutUint32(body[offset+8:offset+12], math.Float32bits(high))
		binary.LittleEndian.PutUint32(body[offset+12:offset+16], math.Float32bits(low))
		binary.LittleEndian.PutUint32(body[offset+16:offset+20], math.Float32bits(close))
		binary.LittleEndian.PutUint32(body[offset+20:offset+24], math.Float32bits(amount))
		binary.LittleEndian.PutUint32(body[offset+24:offset+28], volume)
	}

	putRecord(42, 20240801, 1.2624, 1.2624, 1.2624, 1.2624, 0, 0)
	putRecord(74, 20240802, 1.2449, 1.2501, 1.2403, 1.2468, 12.5, 3456)

	klines, err := DecodeFundKlines(body, "009708", KlinePeriodDay)
	if err != nil {
		t.Fatalf("DecodeFundKlines error: %v", err)
	}
	if len(klines) != 2 {
		t.Fatalf("DecodeFundKlines len = %d, want 2", len(klines))
	}
	if got := klines[0].Time.Format("2006-01-02"); got != "2024-08-01" {
		t.Fatalf("first date = %s, want 2024-08-01", got)
	}
	if math.Abs(klines[0].Open-1.2624) > 1e-6 || math.Abs(klines[0].Close-1.2624) > 1e-6 {
		t.Fatalf("first kline prices = %+v", klines[0])
	}
	if got := klines[1].Time.Format("2006-01-02"); got != "2024-08-02" {
		t.Fatalf("second date = %s, want 2024-08-02", got)
	}
	if klines[1].Volume != 3456 {
		t.Fatalf("second volume = %d, want 3456", klines[1].Volume)
	}
	if math.Abs(klines[1].Amount-12.5) > 1e-6 {
		t.Fatalf("second amount = %v, want 12.5", klines[1].Amount)
	}
}
