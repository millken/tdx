package tdx

import "encoding/binary"

// Handshake protocol constants.
const (
	ServiceIDPing  uint16 = 0x0000
	PingCMD        uint16 = 0x0015
	ServiceIDAuth  uint16 = 0x1894
	ServiceIDStage uint16 = 0x1899
	Stage2CMD      uint16 = 0x0FDB
)

// PingRequest builds the 12-byte ping/probe packet (0x0015).
func PingRequest(seq uint16) []byte {
	h := WrapperHeader{
		Magic:     0x0C,
		Type:      0x00,
		ServiceID: ServiceIDPing,
		PacketSeq: seq,
		Len1:      0x0002,
		Len2:      0x0002,
		CMD:       PingCMD,
	}
	return h.Serialize()
}

// ConnectAuthRequest builds the 13-byte connect-auth packet (0x1894/0x000D).
func ConnectAuthRequest(seq uint16) []byte {
	packet := make([]byte, 13)
	packet[0] = 0x0C
	packet[1] = 0x02
	binary.BigEndian.PutUint16(packet[2:4], ServiceIDAuth)
	binary.BigEndian.PutUint16(packet[4:6], seq)
	binary.LittleEndian.PutUint16(packet[6:8], 0x0003)
	binary.LittleEndian.PutUint16(packet[8:10], 0x0003)
	binary.LittleEndian.PutUint16(packet[10:12], 0x000D)
	packet[12] = 0x01
	return packet
}

// Stage2Request builds the stage2 session-init packet (0x1899/0x0FDB).
func Stage2Request(seq uint16) []byte {
	payload := []byte{
		0x74, 0x64, 0x78, 0x6c, 0x65, 0x76, 0x65, 0x6c, // "tdxlevel"
		0x00, 0x00, 0x00,
		0xe1, 0x7a, 0xf4, 0x40,
		0x4c,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x05,
	}
	packet := make([]byte, 12+len(payload))
	packet[0] = 0x0C
	packet[1] = 0x03
	binary.BigEndian.PutUint16(packet[2:4], ServiceIDStage)
	binary.BigEndian.PutUint16(packet[4:6], seq)
	binary.LittleEndian.PutUint16(packet[6:8], uint16(len(payload)+2))
	binary.LittleEndian.PutUint16(packet[8:10], uint16(len(payload)+2))
	binary.LittleEndian.PutUint16(packet[10:12], Stage2CMD)
	copy(packet[12:], payload)
	return packet
}

// SP mode constants.
const (
	SPLoginCMD       uint16 = 0x2454
	FundBootstrapCMD uint16 = 0x23F0 // wire bytes: f0 23
	FundDetailCMD    uint16 = 0x2488 // wire bytes: 88 24
	FundKlineCMD     uint16 = 0x2489 // wire bytes: 89 24
)

// SPLoginBody is the encrypted login body for SP mode (80 bytes).
var SPLoginBody = []byte{
	0xe5, 0xbb, 0x1c, 0x2f, 0xaf, 0xe5, 0x25, 0x94,
	0x1f, 0x32, 0xc6, 0xe5, 0xd5, 0x3d, 0xfb, 0x41,
	0x5b, 0x73, 0x4c, 0xc9, 0xcd, 0xbf, 0x0a, 0xc9,
	0x20, 0x21, 0xbf, 0xdd, 0x1e, 0xb0, 0x6d, 0x22,
	0xd0, 0x08, 0x88, 0x4c, 0x16, 0x11, 0xcb, 0x13,
	0x78, 0xf6, 0xab, 0xd8, 0x24, 0xd8, 0x99, 0xd2,
	0x1f, 0x32, 0xc6, 0xe5, 0xd5, 0x3d, 0xfb, 0x41,
	0x1f, 0x32, 0xc6, 0xe5, 0xd5, 0x3d, 0xfb, 0x41,
	0xa9, 0x32, 0x5a, 0xc9, 0x35, 0xdc, 0x08, 0x37,
	0x33, 0x5a, 0x16, 0xe4, 0xce, 0x17, 0xc1, 0xbb,
}

// SPLoginRequest builds the SP mode login packet (0x2454) using head=0x01 framing.
func SPLoginRequest(seq uint16) []byte {
	return BuildSPFrame(0x01, SPLoginCMD, SPLoginBody)
}

// FundBootstrapRequest builds the 0x23F0 fund bootstrap packet using 0x01 framing.
func FundBootstrapRequest(seq uint16) []byte {
	return BuildSPFrame(0x01, FundBootstrapCMD, nil)
}
