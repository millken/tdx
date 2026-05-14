package tdx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// MarketCode represents a single security in the code list response.
type MarketCode struct {
	Market    uint16
	Code      string
	Name      string
	Multiple  uint16
	Decimal   int8
	LastPrice float64
}

// MarketCodePage is one page of the code list response.
type MarketCodePage struct {
	Count uint16
	List  []MarketCode
}

// RequestCodeListFrame builds a 0x0450 code list request frame.
func RequestCodeListFrame(msgID uint32, control byte, market uint16, start uint16) ([]byte, error) {
	body := make([]byte, 4)
	body[0] = byte(market)
	binary.LittleEndian.PutUint16(body[2:4], start)
	return BuildDirectFrame(msgID, control, DirectFrameTypeCodeList, body), nil
}

// GetMarketCodes retrieves one page of stock codes for the given market.
//
// Parameters:
//   - market: MarketShenzhen (0), MarketShanghai (1), or MarketBeijing (2)
//   - start: page offset (0, 1000, 2000, ...)
//
// Example:
//
//	page, err := c.GetMarketCodes(MarketShanghai, 0)
func (c *MainClient) GetMarketCodes(market uint16, start uint16) (*MarketCodePage, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	packet, err := RequestCodeListFrame(0x01020304, 0x01, market, start)
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeCodeList, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty code list response")
	}

	return DecodeMarketCodeList(response.Body.Decoded, market)
}

// DecodeMarketCodeList decodes a 0x0450 code list response body.
func DecodeMarketCodeList(body []byte, market uint16) (*MarketCodePage, error) {
	if len(body) < 2 {
		return nil, fmt.Errorf("code body too short: %d", len(body))
	}
	count := int(binary.LittleEndian.Uint16(body[:2]))
	body = body[2:]
	if len(body) < count*29 {
		return nil, fmt.Errorf("code body length mismatch: got=%d want>=%d", len(body), count*29)
	}
	list := make([]MarketCode, 0, count)
	for i := 0; i < count; i++ {
		record := body[i*29 : (i+1)*29]
		code := strings.TrimSpace(trimNulASCII(record[:6]))
		name := trimNulCodeGBK(record[8:16])
		list = append(list, MarketCode{
			Market:    market,
			Code:      code,
			Name:      name,
			Multiple:  binary.LittleEndian.Uint16(record[6:8]),
			Decimal:   int8(record[20]),
			LastPrice: decodeTDXVolume(binary.LittleEndian.Uint32(record[21:25])),
		})
	}
	return &MarketCodePage{Count: uint16(count), List: list}, nil
}

func trimNulASCII(data []byte) string {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	return string(data)
}

func trimNulCodeGBK(data []byte) string {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	decoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), data)
	if err != nil {
		return strings.TrimSpace(string(data))
	}
	return strings.TrimSpace(string(decoded))
}
