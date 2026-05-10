package tdx

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestDecodeBoardMembersMainNetAmount(t *testing.T) {
	body := make([]byte, 26)
	body[13] = 0x08
	binary.LittleEndian.PutUint32(body[20:24], 1)
	binary.LittleEndian.PutUint16(body[24:26], 1)

	row := make([]byte, 72)
	binary.LittleEndian.PutUint16(row[0:2], MarketShanghai)
	copy(row[2:24], []byte("688017"))
	copy(row[24:68], []byte("绿的谐波"))
	binary.LittleEndian.PutUint32(row[68:72], math.Float32bits(290817280))
	body = append(body, row...)

	items, err := DecodeBoardMembers(body)
	if err != nil {
		t.Fatalf("DecodeBoardMembers() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("DecodeBoardMembers() len = %d, want 1", len(items))
	}
	if items[0].Code != "688017" {
		t.Fatalf("code = %q, want 688017", items[0].Code)
	}
	if items[0].MainNetAmount != 290817280 {
		t.Fatalf("main_net = %v, want 290817280", items[0].MainNetAmount)
	}
}
