package tdx

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// ---------------------------------------------------------------------------
// Wrapper frame (0x0C / 0x0B) — 12-byte outer header
// ---------------------------------------------------------------------------

// WrapperHeader is the 12-byte outer header for request packets.
type WrapperHeader struct {
	Magic     uint8
	Type      uint8
	ServiceID uint16
	PacketSeq uint16
	Len1      uint16
	Len2      uint16
	CMD       uint16
}

// Serialize encodes the 12-byte wrapper header.
func (h *WrapperHeader) Serialize() []byte {
	buf := make([]byte, 12)
	buf[0] = h.Magic
	buf[1] = h.Type
	binary.BigEndian.PutUint16(buf[2:4], h.ServiceID)
	binary.LittleEndian.PutUint16(buf[4:6], h.PacketSeq)
	binary.LittleEndian.PutUint16(buf[6:8], h.Len1)
	binary.LittleEndian.PutUint16(buf[8:10], h.Len2)
	binary.LittleEndian.PutUint16(buf[10:12], h.CMD)
	return buf
}

// ---------------------------------------------------------------------------
// Response frame (0xB1CB7400) — 16-byte response header
// ---------------------------------------------------------------------------

// ResponseHeader is the 16-byte header of server response packets.
type ResponseHeader struct {
	Magic     uint32
	Unknown1  uint16
	ServiceID uint16
	PacketSeq uint16
	CMD       uint16
	ZipLen    uint16
	RawLen    uint16
}

// DecodeResponseHeader parses a 16-byte response header.
func DecodeResponseHeader(data []byte) (*ResponseHeader, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("response header too short: %d", len(data))
	}
	h := &ResponseHeader{
		Magic:     binary.BigEndian.Uint32(data[0:4]),
		Unknown1:  binary.LittleEndian.Uint16(data[4:6]),
		ServiceID: binary.BigEndian.Uint16(data[6:8]),
		PacketSeq: binary.LittleEndian.Uint16(data[8:10]),
		CMD:       binary.LittleEndian.Uint16(data[10:12]),
		ZipLen:    binary.LittleEndian.Uint16(data[12:14]),
		RawLen:    binary.LittleEndian.Uint16(data[14:16]),
	}
	if h.Magic != 0xB1CB7400 {
		return nil, fmt.Errorf("invalid response magic: 0x%08X", h.Magic)
	}
	return h, nil
}

// ---------------------------------------------------------------------------
// Response packet
// ---------------------------------------------------------------------------

// DecodedBody holds the decoded payload of a response.
type DecodedBody struct {
	WirePayload []byte
	Decoded     []byte
}

// ResponsePacket is a fully decoded server response.
type ResponsePacket struct {
	Header *ResponseHeader
	Body   *DecodedBody
}

// DecodeResponsePacket decodes a full response from header + payload bytes.
func DecodeResponsePacket(headerData, payload []byte) (*ResponsePacket, error) {
	header, err := DecodeResponseHeader(headerData)
	if err != nil {
		return nil, err
	}
	body := &DecodedBody{
		WirePayload: append([]byte(nil), payload...),
		Decoded:     payload,
	}
	if header.ZipLen > 0 && header.ZipLen != header.RawLen {
		decoded, err := Decompress(payload)
		if err == nil {
			body.Decoded = decoded
		}
	}
	return &ResponsePacket{Header: header, Body: body}, nil
}

// ---------------------------------------------------------------------------
// Direct frame (0x0C) — used for business requests like kline, tick, etc.
// ---------------------------------------------------------------------------

// Direct frame type constants.
const (
	DirectFrameTypeCodeList     uint16 = 0x0450
	DirectFrameTypeKLineOffset  uint16 = 0x052D
	DirectFrameTypeTick         uint16 = 0x053E
	DirectFrameTypeTickChart    uint16 = 0x0537
	DirectFrameTypeTransaction  uint16 = 0x0FC5
	DirectFrameTypeHistoryTrade uint16 = 0x0FB5
	DirectFrameTypeXdXr         uint16 = 0x000F
	DirectFrameTypeFinance      uint16 = 0x0010
	DirectFrameTypeCompanyCat   uint16 = 0x02CF
	DirectFrameTypeCompanyCtn   uint16 = 0x02D0
	DirectFrameTypeTopBoard     uint16 = 0x053F
	DirectFrameTypeQuotesList   uint16 = 0x054B
	DirectFrameTypeBoardMembers   uint16 = 0x122C
	DirectFrameTypeBatchQuote     uint16 = 0x054C

	// MAC (mac_quotation) 协议命令码，经 SP 登录后走 head=0x01 帧。
	DirectFrameTypeMACSymbolInfo    uint16 = 0x122A // MAC: 股票摘要
	DirectFrameTypeMACSymbolQuotes  uint16 = 0x122B // MAC: 批量股票报价
	DirectFrameTypeMACQuotes        uint16 = 0x122D // MAC: 行情快照(含分时)
	DirectFrameTypeMACSymbolBars    uint16 = 0x122E // MAC: 统一K线
	DirectFrameTypeMACTransactions  uint16 = 0x122F // MAC: 分时成交
	DirectFrameTypeMACMarketMonitor uint16 = 0x1237 // MAC: 市场监控
	DirectFrameTypeMACCapitalFlow   uint16 = 0x1218 // MAC: 资金流向(head=0x02)
	DirectFrameTypeMACTickCharts    uint16 = 0x123E // MAC: 多日分时
)

// Market constants.
const (
	MarketShenzhen uint16 = 0
	MarketShanghai uint16 = 1
	MarketBeijing  uint16 = 2
)

// MarketString returns a short market label ("SH", "SZ", "BJ") for the given market code.
func MarketString(m uint16) string {
	switch m {
	case 1:
		return "SH"
	case 0:
		return "SZ"
	case 2:
		return "BJ"
	default:
		return fmt.Sprintf("%d", m)
	}
}

// BuildDirectFrame constructs a 12-byte header + body direct frame.
func BuildDirectFrame(msgID uint32, control byte, frameType uint16, body []byte) []byte {
	packet := make([]byte, 12+len(body))
	packet[0] = 0x0C
	binary.LittleEndian.PutUint32(packet[1:5], msgID)
	packet[5] = control
	binary.LittleEndian.PutUint16(packet[6:8], uint16(len(body)+2))
	binary.LittleEndian.PutUint16(packet[8:10], uint16(len(body)+2))
	binary.LittleEndian.PutUint16(packet[10:12], frameType)
	copy(packet[12:], body)
	return packet
}

// BuildSPFrame constructs an SP mode frame (head=0x01) used by mac_quotation protocols.
// Wire format: [head:1][customize:4][control:1][len:2][len:2][msg_id:2][body...]
func BuildSPFrame(head byte, msgID uint16, body []byte) []byte {
	innerBody := make([]byte, 2+len(body))
	binary.LittleEndian.PutUint16(innerBody[0:2], msgID)
	copy(innerBody[2:], body)

	header := make([]byte, 10)
	header[0] = head
	binary.LittleEndian.PutUint32(header[1:5], 0) // customize
	header[5] = 1                                 // control
	binary.LittleEndian.PutUint16(header[6:8], uint16(len(innerBody)))
	binary.LittleEndian.PutUint16(header[8:10], uint16(len(innerBody)))

	return append(header, innerBody...)
}

// ---------------------------------------------------------------------------
// Compression
// ---------------------------------------------------------------------------

// Decompress decompresses zlib data.
func Decompress(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
