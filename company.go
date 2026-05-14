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

// CompanyCategoryItem represents one section in the F10 information directory.
type CompanyCategoryItem struct {
	Name     string // Section name (e.g. "公司概况")
	Filename string // Section filename
	Start    uint32 // Content start offset
	Length   uint32 // Content length in bytes
}

// RequestCompanyCategoryFrame builds a 0x02CF company category request frame.
func RequestCompanyCategoryFrame(msgID uint32, control byte, market uint16, code string) ([]byte, error) {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint16(body[0:2], market)
	copy(body[2:8], code)
	binary.LittleEndian.PutUint32(body[8:12], 0)
	return BuildDirectFrame(msgID, control, DirectFrameTypeCompanyCat, body), nil
}

// GetCompanyCategory retrieves the F10 information section list.
func (c *MainClient) GetCompanyCategory(code string) ([]CompanyCategoryItem, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	normalizedCode, market, hasPrefixedMarket, err := normalizeKlineCode(code)
	if err != nil {
		return nil, err
	}
	if !hasPrefixedMarket {
		market, err = inferKlineMarket(normalizedCode)
		if err != nil {
			return nil, err
		}
	}

	packet, err := RequestCompanyCategoryFrame(0x000A0401, 0x01, market, normalizedCode)
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, DirectFrameTypeCompanyCat, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty company category response")
	}

	return DecodeCompanyCategory(response.Body.Decoded)
}

// DecodeCompanyCategory decodes a 0x02CF company category response body.
//
// Layout: uint16 count, then count * 152-byte records:
//
//	name[64] + filename[80] + start(uint32) + length(uint32)
func DecodeCompanyCategory(body []byte) ([]CompanyCategoryItem, error) {
	if len(body) < 2 {
		return nil, fmt.Errorf("company category body too short: %d", len(body))
	}

	count := int(binary.LittleEndian.Uint16(body[:2]))
	body = body[2:]

	const recordSize = 152
	items := make([]CompanyCategoryItem, 0, count)
	for i := 0; i < count && len(body) >= recordSize; i++ {
		name := trimNulCodeGBK(body[:64])
		filename := trimNulCodeGBK(body[64:144])
		start := binary.LittleEndian.Uint32(body[144:148])
		length := binary.LittleEndian.Uint32(body[148:152])
		items = append(items, CompanyCategoryItem{
			Name:     name,
			Filename: filename,
			Start:    start,
			Length:   length,
		})
		body = body[recordSize:]
	}

	return items, nil
}

// RequestCompanyContentFrame builds a 0x02D0 company content request frame.
func RequestCompanyContentFrame(msgID uint32, control byte, market uint16, code string, filename string, start uint32, length uint32) ([]byte, error) {
	body := make([]byte, 102)
	binary.LittleEndian.PutUint16(body[0:2], market)
	copy(body[2:8], code)
	binary.LittleEndian.PutUint16(body[8:10], 0)
	copy(body[10:90], filename)
	binary.LittleEndian.PutUint32(body[90:94], start)
	binary.LittleEndian.PutUint32(body[94:98], length)
	return BuildDirectFrame(msgID, control, DirectFrameTypeCompanyCtn, body), nil
}

// GetCompanyContent retrieves F10 section content for the given stock code.
func (c *MainClient) GetCompanyContent(code string, filename string, start uint32, length uint32) (string, error) {
	if err := c.ensureConn(); err != nil {
		return "", err
	}

	normalizedCode, market, hasPrefixedMarket, err := normalizeKlineCode(code)
	if err != nil {
		return "", err
	}
	if !hasPrefixedMarket {
		market, err = inferKlineMarket(normalizedCode)
		if err != nil {
			return "", err
		}
	}

	packet, err := RequestCompanyContentFrame(0x000A0401, 0x01, market, normalizedCode, filename, start, length)
	if err != nil {
		return "", err
	}

	response, err := c.do(packet, DirectFrameTypeCompanyCtn, 8*time.Second)
	if err != nil {
		return "", err
	}

	if response == nil || response.Body == nil {
		return "", fmt.Errorf("tdx: empty company content response")
	}

	return DecodeCompanyContent(response.Body.Decoded)
}

// DecodeCompanyContent decodes a 0x02D0 company content response body.
//
// Layout: "<H6sHH" (12 bytes header), then content bytes (GBK encoded).
func DecodeCompanyContent(body []byte) (string, error) {
	if len(body) < 12 {
		return "", fmt.Errorf("company content body too short: %d", len(body))
	}

	length := int(binary.LittleEndian.Uint16(body[10:12]))
	if len(body) < 12+length {
		length = len(body) - 12
	}

	raw := body[12 : 12+length]
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		raw = raw[:i]
	}

	decoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), raw)
	if err != nil {
		return strings.TrimSpace(string(raw)), nil
	}
	return strings.TrimSpace(string(decoded)), nil
}

// suppress unused import
var _ = time.Local
