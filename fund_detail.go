package tdx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

const defaultFundDetailMode uint16 = 50

// FundDetail represents a 0x2488 fund detail response.
//
// The response format is verified, but the semantic meaning of each item ID
// and the six uint16 values is not fully mapped yet, so the raw structure is
// exposed directly.
type FundDetail struct {
	Category byte
	Code     string
	Items    []FundDetailItem
}

// FundDetailItem represents one 16-byte row in a 0x2488 response.
type FundDetailItem struct {
	ID     uint32
	Values [6]uint16
}

// FindItem returns the first detail row matching id.
func (d *FundDetail) FindItem(id uint32) (FundDetailItem, bool) {
	if d == nil {
		return FundDetailItem{}, false
	}
	for _, item := range d.Items {
		if item.ID == id {
			return item, true
		}
	}
	return FundDetailItem{}, false
}

// GetFundDetail retrieves fund detail data via the 7727 extension quote 0x2488 protocol.
func (c *MainClient) GetFundDetail(code string) (*FundDetail, error) {
	return c.GetFundDetailMode(code, defaultFundDetailMode)
}

// GetFundDetail retrieves fund detail data via the 7727 extension quote 0x2488 protocol.
func (c *ExClient) GetFundDetail(code string) (*FundDetail, error) {
	return c.GetFundDetailMode(code, defaultFundDetailMode)
}

// GetFundDetailMode retrieves fund detail data via the 7727 extension quote 0x2488 protocol
// using the provided mode.
func (c *MainClient) GetFundDetailMode(code string, mode uint16) (*FundDetail, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	if _, market, _, err := normalizeKlineCode(code); err == nil && market == MarketBeijing {
		return nil, fmt.Errorf("tdx: fund detail does not support Beijing market code %q", code)
	}

	normalizedCode, _, _, err := normalizeKlineCode(code)
	if err != nil {
		return nil, err
	}

	exClient, err := DialExBest(nil, WithTimeout(c.timeout))
	if err != nil {
		return nil, err
	}
	defer exClient.Close()

	return exClient.GetFundDetailMode(normalizedCode, mode)
}

// GetFundDetailMode retrieves fund detail data via the 7727 extension quote 0x2488 protocol
// using the provided mode.
func (c *ExClient) GetFundDetailMode(code string, mode uint16) (*FundDetail, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	if _, market, _, err := normalizeKlineCode(code); err == nil && market == MarketBeijing {
		return nil, fmt.Errorf("tdx: fund detail does not support Beijing market code %q", code)
	}

	normalizedCode, _, _, err := normalizeKlineCode(code)
	if err != nil {
		return nil, err
	}

	packet, err := RequestFundDetailFrame(normalizedCode, mode)
	if err != nil {
		return nil, err
	}

	response, err := c.do(packet, FundDetailCMD, c.timeout)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty fund detail response")
	}

	return DecodeFundDetail(response.Body.Decoded)
}

// RequestFundDetailFrame builds a 0x2488 fund detail request frame.
func RequestFundDetailFrame(code string, mode uint16) ([]byte, error) {
	if len(code) != 6 {
		return nil, fmt.Errorf("tdx: invalid fund code %q", code)
	}
	if mode == 0 {
		mode = defaultFundDetailMode
	}
	market, _ := inferKlineMarket(code)
	body := make([]byte, 38)
	body[0] = inferFundCategory(code, market)
	copy(body[1:24], code)
	binary.LittleEndian.PutUint16(body[28:30], mode)
	return BuildSPFrame(0x01, FundDetailCMD, body), nil
}

// DecodeFundDetail decodes a 0x2488 fund detail response body.
func DecodeFundDetail(body []byte) (*FundDetail, error) {
	if len(body) < 38 {
		return nil, fmt.Errorf("fund detail body too short: %d", len(body))
	}

	count := int(binary.LittleEndian.Uint16(body[36:38]))
	needed := 38 + count*16
	if len(body) < needed {
		return nil, fmt.Errorf("fund detail body truncated: len=%d want>=%d", len(body), needed)
	}

	detail := &FundDetail{
		Category: body[0],
		Code:     decodeFundDetailCode(body[1:36]),
		Items:    make([]FundDetailItem, 0, count),
	}

	pos := 38
	for i := 0; i < count; i++ {
		itemBody := body[pos : pos+16]
		item := FundDetailItem{
			ID: binary.LittleEndian.Uint32(itemBody[0:4]),
		}
		for j := range item.Values {
			item.Values[j] = binary.LittleEndian.Uint16(itemBody[4+j*2 : 6+j*2])
		}
		detail.Items = append(detail.Items, item)
		pos += 16
	}

	return detail, nil
}

func decodeFundDetailCode(data []byte) string {
	trimmed := bytes.TrimRight(data, "\x00")
	return strings.TrimSpace(string(trimmed))
}
