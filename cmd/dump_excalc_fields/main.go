// dump_excalc_fields: 转储 G_REAL_HQ 前 N 条记录的所有 Protobuf 字段
// 用于发现 3日/5日涨幅等未知字段
package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
)

const host = "excalc2.icfqs.com:7616"
const dumpN = 3 // 转储前 N 条记录

func main() {
	body, err := fetch("http://" + host + "/site/statics/jsdata/real_hq.js")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	items, err := extractBase64Array(body)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("total batches: %d\n\n", len(items))

	count := 0
	for _, batch := range items {
		if count >= dumpN {
			break
		}
		dumpBatch(batch, &count)
	}
}

func dumpBatch(data []byte, count *int) {
	pos := 0
	for pos < len(data) {
		tag, n := pbVarint(data, pos)
		if n <= 0 {
			break
		}
		pos += n
		field := tag >> 3
		wire := tag & 0x7
		if field == 1 && wire == 2 {
			// sub-message
			length, n2 := pbVarint(data, pos)
			if n2 <= 0 {
				break
			}
			pos += n2
			end := pos + int(length)
			if end > len(data) {
				break
			}
			if *count < dumpN {
				dumpRecord(data[pos:end], *count)
				*count++
			}
			pos = end
		} else {
			pbSkip(data, &pos, wire)
		}
	}
}

func dumpRecord(data []byte, idx int) {
	// 先找 code
	code := ""
	pos := 0
	for pos < len(data) {
		tag, n := pbVarint(data, pos)
		if n <= 0 {
			break
		}
		pos += n
		field := tag >> 3
		wire := tag & 0x7
		if field == 2 && wire == 2 {
			s, sn, err := pbString(data, pos)
			if err == nil {
				code = s
			}
			pos += sn
		} else {
			pbSkip(data, &pos, wire)
		}
	}

	fmt.Printf("=== Record[%d] code=%s (len=%d) ===\n", idx, code, len(data))

	// 再完整转储所有字段
	pos = 0
	for pos < len(data) {
		tag, n := pbVarint(data, pos)
		if n <= 0 {
			break
		}
		pos += n
		field := tag >> 3
		wire := tag & 0x7
		switch wire {
		case 0: // varint
			v, vn := pbVarint(data, pos)
			fmt.Printf("  field=%2d wire=0(varint)  value=%d\n", field, v)
			pos += vn
		case 1: // 64-bit
			if pos+8 <= len(data) {
				u := binary.LittleEndian.Uint64(data[pos:])
				f := math.Float64frombits(u)
				fmt.Printf("  field=%2d wire=1(64bit)   uint64=%d  float64=%.6g\n", field, u, f)
				pos += 8
			}
		case 2: // length-delimited
			length, ln := pbVarint(data, pos)
			pos += ln
			end := pos + int(length)
			if end > len(data) {
				end = len(data)
			}
			s := string(data[pos:end])
			fmt.Printf("  field=%2d wire=2(bytes)    len=%d  str=%q\n", field, length, s)
			pos = end
		case 5: // 32-bit
			if pos+4 <= len(data) {
				u := binary.LittleEndian.Uint32(data[pos:])
				f := math.Float32frombits(u)
				fmt.Printf("  field=%2d wire=5(32bit)   uint32=0x%08x  float32=%.6g\n", field, u, f)
				pos += 4
			}
		default:
			fmt.Printf("  field=%2d wire=%d(unknown) — stopping\n", field, wire)
			pos = len(data)
		}
	}
	fmt.Println()
}

// ── Protobuf helpers ──────────────────────────────────────────

func pbVarint(data []byte, pos int) (uint64, int) {
	var x uint64
	var s uint
	for i := pos; i < len(data); i++ {
		b := data[i]
		x |= uint64(b&0x7f) << s
		s += 7
		if b < 0x80 {
			return x, i - pos + 1
		}
	}
	return 0, -1
}

func pbString(data []byte, pos int) (string, int, error) {
	length, n := pbVarint(data, pos)
	if n <= 0 {
		return "", 0, fmt.Errorf("truncated string length")
	}
	pos += n
	end := pos + int(length)
	if end > len(data) {
		return "", 0, fmt.Errorf("truncated string body")
	}
	return string(data[pos:end]), n + int(length), nil
}

func pbSkip(data []byte, pos *int, wire uint64) error {
	switch wire {
	case 0:
		_, n := pbVarint(data, *pos)
		if n <= 0 {
			return fmt.Errorf("truncated varint")
		}
		*pos += n
	case 1:
		*pos += 8
	case 2:
		length, n := pbVarint(data, *pos)
		if n <= 0 {
			return fmt.Errorf("truncated length")
		}
		*pos += n + int(length)
	case 5:
		*pos += 4
	default:
		return fmt.Errorf("unknown wire type %d", wire)
	}
	return nil
}

// ── HTTP fetch ────────────────────────────────────────────────

func fetch(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ── JS base64 array extractor ─────────────────────────────────

var reBase64 = regexp.MustCompile(`"([A-Za-z0-9+/]+=*)"`)

func extractBase64Array(body []byte) ([][]byte, error) {
	matches := reBase64.FindAllSubmatch(body, -1)
	var out [][]byte
	for _, m := range matches {
		dec, err := base64.StdEncoding.DecodeString(string(m[1]))
		if err != nil {
			continue
		}
		out = append(out, dec)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no base64 data found")
	}
	return out, nil
}
