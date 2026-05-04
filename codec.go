package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// ---------------------------------------------------------------------------
// Variable-length price codec (varint with sign bit)
// ---------------------------------------------------------------------------

// cutPrice reads a variable-length encoded price delta from data.
// Returns remaining data and the decoded signed value.
func cutPrice(data []byte) ([]byte, int64, error) {
	for i := range data {
		if data[i]&0x80 != 0 {
			continue
		}
		return data[i+1:], decodeVarintPrice(data[:i+1]), nil
	}
	return nil, 0, fmt.Errorf("unterminated price varint")
}

// decodeVarintPrice decodes a signed variable-length integer.
// First byte: bit7=continuation, bit6=sign, bits5-0=value.
// Subsequent bytes: bit7=continuation, bits6-0=value.
func decodeVarintPrice(data []byte) int64 {
	value := int64(0)
	for i := range data {
		switch i {
		case 0:
			value += int64(data[0] & 0x3F)
		default:
			value += int64(data[i]&0x7F) << uint(6+(i-1)*7)
		}
		if data[i]&0x80 == 0 {
			break
		}
	}
	if len(data) > 0 && data[0]&0x40 != 0 {
		value = -value
	}
	return value
}

// cutVarintInt reads a variable-length encoded signed integer.
func cutVarintInt(data []byte) ([]byte, int64, error) {
	for i := range data {
		if data[i]&0x80 != 0 {
			continue
		}
		return data[i+1:], decodeVarintPrice(data[:i+1]), nil
	}
	return nil, 0, fmt.Errorf("unterminated int varint")
}

// cutVarintUint reads a variable-length encoded unsigned integer.
func cutVarintUint(data []byte) ([]byte, uint64, error) {
	for i := range data {
		if data[i]&0x80 != 0 {
			continue
		}
		return data[i+1:], decodeVarintUint(data[:i+1]), nil
	}
	return nil, 0, fmt.Errorf("unterminated uint varint")
}

// decodeVarintUint decodes an unsigned variable-length integer.
func decodeVarintUint(data []byte) uint64 {
	value := uint64(0)
	for i := range data {
		switch i {
		case 0:
			value += uint64(data[0] & 0x3F)
		default:
			value += uint64(data[i]&0x7F) << uint(6+(i-1)*7)
		}
		if data[i]&0x80 == 0 {
			break
		}
	}
	return value
}

// ---------------------------------------------------------------------------
// TDX volume codec
// ---------------------------------------------------------------------------

// decodeTDXVolume decodes the TDX-specific 4-byte volume/amount encoding.
func decodeTDXVolume(value uint32) float64 {
	ivol := int32(value)
	logpoint := ivol >> 24
	high := (ivol >> 16) & 0xff
	mid := (ivol >> 8) & 0xff
	low := ivol & 0xff

	base := math.Exp2(float64(logpoint*2 - 0x7f))

	accum := base
	if high > 0x80 {
		accum = base * (64.0 + float64(high&0x7f)) / 64.0
		accum += base
	} else {
		accum += base * float64(high) / 128.0
	}

	scale := 1.0
	if high&0x80 != 0 {
		scale = 2.0
	}

	accum += base * float64(mid) / 32768.0 * scale
	accum += base * float64(low) / 8388608.0 * scale
	return accum
}

// ---------------------------------------------------------------------------
// Time codec
// ---------------------------------------------------------------------------

// decodeKlineTime decodes a 4-byte time field from a kline record.
func decodeKlineTime(data []byte, period uint16) time.Time {
	if len(data) < 4 {
		return time.Time{}
	}
	switch period {
	case KlinePeriod1Minute, KlinePeriodMultiMin, KlinePeriod5Minute, KlinePeriod15Minute, KlinePeriod30Minute, KlinePeriod60Minute:
		yearMonthDay := binary.LittleEndian.Uint16(data[:2])
		hourMinute := binary.LittleEndian.Uint16(data[2:4])
		dayBits := yearMonthDay % 2048
		year := int(yearMonthDay>>11) + 2004
		month := int(dayBits / 100)
		day := int(dayBits % 100)
		hour := int(hourMinute / 60)
		minute := int(hourMinute % 60)
		return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.Local)
	default:
		yearMonthDay := binary.LittleEndian.Uint32(data[:4])
		year := int(yearMonthDay / 10000)
		month := int((yearMonthDay % 10000) / 100)
		day := int(yearMonthDay % 100)
		return time.Date(year, time.Month(month), day, 15, 0, 0, 0, time.Local)
	}
}

// ---------------------------------------------------------------------------
// Unit conversion
// ---------------------------------------------------------------------------

// milliToYuan converts milli-yuan (1/1000 yuan) to yuan.
func milliToYuan(value int64) float64 {
	return float64(value) / 1000
}

// requiresMinuteVolumeScaling returns true for minute-level kline periods
// that need volume divided by 100.
func requiresMinuteVolumeScaling(period uint16) bool {
	switch period {
	case KlinePeriod1Minute, KlinePeriod5Minute, KlinePeriod15Minute, KlinePeriod30Minute, KlinePeriod60Minute, KlinePeriodMultiMin:
		return true
	default:
		return false
	}
}
