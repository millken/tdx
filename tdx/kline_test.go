package tdx

import (
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
