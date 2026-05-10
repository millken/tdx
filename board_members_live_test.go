package tdx

import (
	"encoding/binary"
	"math"
	"os"
	"sort"
	"strconv"
	"testing"
)

type boardMembersProbeScore struct {
	Bit        int
	Kind       string
	Violations int
	First      float64
	Last       float64
}

func TestProbeBoardMembersUnknownBitmapMainNet(t *testing.T) {
	if os.Getenv("TDX_LIVE") == "" {
		t.Skip("set TDX_LIVE=1 to run live board members probe")
	}

	client, err := DialBest(BestSPAddresses(0))
	if err != nil {
		t.Fatalf("DialBest(BestSPAddresses(0)) error = %v", err)
	}
	defer client.Close()

	board := os.Getenv("TDX_PROBE_BOARD")
	if board == "" {
		board = "881314"
	}

	count := 12
	if s := os.Getenv("TDX_PROBE_COUNT"); s != "" {
		if n, convErr := strconv.Atoi(s); convErr == nil && n > 2 {
			count = n
		}
	}

	startBit := 0x40
	if s := os.Getenv("TDX_PROBE_START_BIT"); s != "" {
		if n, convErr := strconv.Atoi(s); convErr == nil && n >= 0 {
			startBit = n
		}
	}
	endBit := 0x9f
	if s := os.Getenv("TDX_PROBE_END_BIT"); s != "" {
		if n, convErr := strconv.Atoi(s); convErr == nil && n >= startBit {
			endBit = n
		}
	}

	known := make(map[int]struct{})
	for _, bit := range getActiveBits(defaultBitmap) {
		known[bit] = struct{}{}
	}

	var scores []boardMembersProbeScore
	for bit := startBit; bit <= endBit; bit++ {
		if _, ok := known[bit]; ok {
			continue
		}

		raws, err := fetchBoardMembersProbeField(client, board, BoardMembersSortMainNetAmount, count, bit)
		if err != nil {
			t.Logf("bit=0x%02X fetch error: %v", bit, err)
			continue
		}
		if len(raws) < 3 {
			continue
		}

		scores = append(scores,
			scoreBoardMembersProbe(bit, "u32", raws, func(v uint32) float64 { return float64(v) }),
			scoreBoardMembersProbe(bit, "i32", raws, func(v uint32) float64 { return float64(int32(v)) }),
			scoreBoardMembersProbe(bit, "f32", raws, func(v uint32) float64 { return float64(math.Float32frombits(v)) }),
		)
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Violations != scores[j].Violations {
			return scores[i].Violations < scores[j].Violations
		}
		if scores[i].Bit != scores[j].Bit {
			return scores[i].Bit < scores[j].Bit
		}
		return scores[i].Kind < scores[j].Kind
	})

	t.Logf("board=%s scanned_bits=[0x%02X,0x%02X] candidates=%d", board, startBit, endBit, len(scores))
	for i := 0; i < len(scores) && i < 20; i++ {
		s := scores[i]
		t.Logf("  bit=0x%02X kind=%-3s violations=%d first=%g last=%g", s.Bit, s.Kind, s.Violations, s.First, s.Last)
	}
}

func fetchBoardMembersProbeField(c *MainClient, board string, sortType uint16, count int, bit int) ([]uint32, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	bitmap := cloneBoardMembersBitmap(defaultBitmap)
	setBoardMembersBitmapBit(bitmap, bit)

	c.drainPending()

	packet, err := RequestBoardMembersFrame(0x000A0401, 0x01, exchangeBoardCode(board), sortType, 0, uint8(count), SortDesc, bitmap)
	if err != nil {
		return nil, err
	}
	if err := c.sendRaw(packet); err != nil {
		return nil, err
	}

	response, err := c.waitForCMD(DirectFrameTypeBoardMembers, defaultTimeout)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, nil
	}

	return decodeBoardMembersProbeField(response.Body.Decoded, bit)
}

func decodeBoardMembersProbeField(body []byte, bit int) ([]uint32, error) {
	if len(body) < 26 {
		return nil, nil
	}

	bitmapEcho := body[:20]
	activeBits := getActiveBits(bitmapEcho)
	fieldIndex := -1
	for i, activeBit := range activeBits {
		if activeBit == bit {
			fieldIndex = i
			break
		}
	}
	if fieldIndex < 0 {
		return nil, nil
	}

	rowCount := int(binary.LittleEndian.Uint16(body[24:26]))
	rowLen := 68 + 4*len(activeBits)
	body = body[26:]
	if rowLen <= 68 || len(body) < rowLen {
		return nil, nil
	}
	if maxRows := len(body) / rowLen; rowCount > maxRows {
		rowCount = maxRows
	}

	offset := 68 + fieldIndex*4
	raws := make([]uint32, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		row := body[i*rowLen : (i+1)*rowLen]
		raws = append(raws, binary.LittleEndian.Uint32(row[offset:offset+4]))
	}
	return raws, nil
}

func scoreBoardMembersProbe(bit int, kind string, raws []uint32, convert func(uint32) float64) boardMembersProbeScore {
	violations := 0
	first := convert(raws[0])
	last := convert(raws[len(raws)-1])
	allEqual := true
	for i := 0; i+1 < len(raws); i++ {
		left := convert(raws[i])
		right := convert(raws[i+1])
		if math.IsNaN(left) || math.IsNaN(right) || math.IsInf(left, 0) || math.IsInf(right, 0) || math.Abs(left) > 1e30 || math.Abs(right) > 1e30 {
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
	return boardMembersProbeScore{Bit: bit, Kind: kind, Violations: violations, First: first, Last: last}
}

func cloneBoardMembersBitmap(src []byte) []byte {
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

func setBoardMembersBitmapBit(bitmap []byte, bit int) {
	if bit < 0 {
		return
	}
	byteIdx := bit / 8
	bitIdx := bit % 8
	if byteIdx >= len(bitmap) {
		return
	}
	bitmap[byteIdx] |= 1 << uint(bitIdx)
}
