package main

import (
	"math"
	"testing"

	"github.com/millken/tdx"
)

func TestParseHotBoardCategory(t *testing.T) {
	tests := []struct {
		input string
		want  uint16
	}{
		{input: "hy", want: tdx.CategoryBoardHY},
		{input: "hy2", want: tdx.CategoryBoardHY2},
		{input: "gn", want: tdx.CategoryBoardGN},
		{input: "fg", want: tdx.CategoryBoardFG},
		{input: "dq", want: tdx.CategoryBoardDQ},
	}

	for _, tt := range tests {
		got, _, err := parseHotBoardCategory(tt.input)
		if err != nil {
			t.Fatalf("parseHotBoardCategory(%q) error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("parseHotBoardCategory(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseHotBoardQuoteSort(t *testing.T) {
	tests := []struct {
		input string
		want  uint16
	}{
		{input: "change_pct", want: tdx.SortChangePct},
		{input: "change_3d", want: tdx.SortChange3dPct},
		{input: "amount", want: tdx.SortAmount},
		{input: "activity", want: tdx.SortActivity},
		{input: "speed_pct", want: tdx.SortSpeedPct},
		{input: "price", want: tdx.SortPrice},
		{input: "code", want: tdx.SortCode},
		{input: "main_net", want: tdx.SortMainNetAmount},
	}

	for _, tt := range tests {
		got, err := parseHotBoardQuoteSort(tt.input)
		if err != nil {
			t.Fatalf("parseHotBoardQuoteSort(%q) error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("parseHotBoardQuoteSort(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseBoardMembersSort(t *testing.T) {
	tests := []struct {
		input string
		want  uint16
	}{
		{input: "change_pct", want: tdx.BoardMembersSortChangePct},
		{input: "code", want: tdx.BoardMembersSortCode},
		{input: "amount", want: tdx.BoardMembersSortAmount},
		{input: "turnover", want: tdx.BoardMembersSortTurnover},
		{input: "vol_ratio", want: tdx.BoardMembersSortVolRatio},
		{input: "main_net", want: tdx.BoardMembersSortMainNetAmount},
	}

	for _, tt := range tests {
		got, err := parseBoardMembersSort(tt.input)
		if err != nil {
			t.Fatalf("parseBoardMembersSort(%q) error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("parseBoardMembersSort(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestMakeHotBoardSnapshot(t *testing.T) {
	got := makeHotBoardSnapshot(tdx.QuotesItem{
		Market:   tdx.MarketShanghai,
		Code:     "880001",
		Price:    105,
		PreClose: 100,
		Amount:   123456789,
		Active:   88,
	}, "上证50")

	if got.Code != "880001" {
		t.Fatalf("code = %q, want 880001", got.Code)
	}
	if got.Name != "上证50" {
		t.Fatalf("name = %q, want 上证50", got.Name)
	}
	if got.ChangePct != 5 {
		t.Fatalf("change_pct = %v, want 5", got.ChangePct)
	}
	if got.Active != 88 {
		t.Fatalf("active = %d, want 88", got.Active)
	}
}

func TestLookupHotBoardName(t *testing.T) {
	names := map[uint16]map[string]string{
		tdx.MarketShanghai: {
			"880001": "上证50",
		},
	}

	if got := lookupHotBoardName(names, tdx.MarketShanghai, "880001"); got != "上证50" {
		t.Fatalf("lookupHotBoardName() = %q, want 上证50", got)
	}
	if got := lookupHotBoardName(names, tdx.MarketShenzhen, "399001"); got != "" {
		t.Fatalf("lookupHotBoardName() = %q, want empty", got)
	}
}

func TestFindBoardHeatmapBoard(t *testing.T) {
	items := []boardHeatmapBoard{{Code: "880001", Name: "上证50"}, {Code: "881314", Name: "机器人"}}

	item, ok := findBoardHeatmapBoard(items, "881314")
	if !ok {
		t.Fatalf("findBoardHeatmapBoard() = not found, want found")
	}
	if item.Name != "机器人" {
		t.Fatalf("findBoardHeatmapBoard() name = %q, want 机器人", item.Name)
	}

	_, ok = findBoardHeatmapBoard(items, "999999")
	if ok {
		t.Fatalf("findBoardHeatmapBoard() unexpected found for missing code")
	}
}

func TestNormalizeBoardTickCode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "881314", want: "sh881314"},
		{input: "399001", want: "sz399001"},
		{input: "899050", want: "bj899050"},
		{input: "sh881314", want: "sh881314"},
	}

	for _, tt := range tests {
		if got := normalizeBoardTickCode(tt.input); got != tt.want {
			t.Fatalf("normalizeBoardTickCode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAssignHotBoardHeat(t *testing.T) {
	entries := []hotBoardEntry{
		{Board: hotBoardSnapshot{Code: "A", ChangePct: 9, Amount: 100, RiseSpeed: 1, Active: 50}},
		{Board: hotBoardSnapshot{Code: "B", ChangePct: 8, Amount: 300, RiseSpeed: 4, Active: 100}},
		{Board: hotBoardSnapshot{Code: "C", ChangePct: 3, Amount: 50, RiseSpeed: 0.5, Active: 20}},
	}

	assignHotBoardHeat(entries)

	if entries[0].Board.Code != "A" {
		t.Fatalf("first board order changed to %q, want A", entries[0].Board.Code)
	}
	if entries[1].Board.Code != "B" {
		t.Fatalf("second board order changed to %q, want B", entries[1].Board.Code)
	}
	if entries[1].Board.Heat <= entries[2].Board.Heat {
		t.Fatalf("board heat = %v, want > last %v", entries[1].Board.Heat, entries[2].Board.Heat)
	}
}

func TestAssignHotStockHeat(t *testing.T) {
	items := []hotStockSnapshot{
		{Code: "000001", ChangePct: 6, Amount: 100, Turnover: 10, VolRatio: 1.2, ConsecutiveUp: 0},
		{Code: "000002", ChangePct: 8, Amount: 300, Turnover: 15, VolRatio: 2.5, ConsecutiveUp: 1},
		{Code: "000003", ChangePct: 2, Amount: 50, Turnover: 5, VolRatio: 0.8, ConsecutiveUp: 0},
	}

	assignHotStockHeat(items)

	if items[0].Code != "000001" {
		t.Fatalf("first stock order changed to %q, want 000001", items[0].Code)
	}
	if items[1].Code != "000002" {
		t.Fatalf("second stock order changed to %q, want 000002", items[1].Code)
	}
	if items[1].Heat <= items[2].Heat {
		t.Fatalf("stock heat = %v, want > last %v", items[1].Heat, items[2].Heat)
	}
}

func TestCalcKlineWindowChangePct(t *testing.T) {
	klines := []tdx.Kline{
		{Close: 1828.90},
		{Close: 1970.00},
		{Close: 2074.80},
		{Close: 2085.02},
	}

	got := calcKlineWindowChangePct(klines, 3)
	want := (2085.02 - 1828.90) / 1828.90 * 100
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("calcKlineWindowChangePct() = %v, want %v", got, want)
	}
}

func TestIsChange3dSort(t *testing.T) {
	if !isChange3dSort("change_3d") {
		t.Fatalf("isChange3dSort(change_3d) = false, want true")
	}
	if !isChange3dSort("strength") {
		t.Fatalf("isChange3dSort(strength) = false, want true")
	}
	if isChange3dSort("change_pct") {
		t.Fatalf("isChange3dSort(change_pct) = true, want false")
	}
}
