package tdx

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Client represents a connection to a TDX 7709 market data server.
type Client struct {
	mu          sync.Mutex
	conn        net.Conn
	addr        string
	closed      bool
	bootstrapped bool
	spMode      bool
	pktSeq      uint32
	packetCh    chan *ResponsePacket
	done        chan struct{}
}

// Dial connects to a TDX server, performs the 3-stage bootstrap handshake
// (Ping → ConnectAuth → Stage2), and returns a ready-to-use Client.
func Dial(addr string, opts ...Option) (*Client, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	conn, err := net.DialTimeout("tcp", addr, cfg.timeout)
	if err != nil {
		return nil, fmt.Errorf("tdx: dial %s: %w", addr, err)
	}

	c := &Client{
		conn:     conn,
		addr:     addr,
		packetCh: make(chan *ResponsePacket, 32),
		done:     make(chan struct{}),
	}

	go c.listen()

	if err := c.bootstrap(cfg.timeout); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("tdx: bootstrap %s: %w", addr, err)
	}

	return c, nil
}

// DialSP connects to a TDX mac_hosts server, performs standard bootstrap + SP mode login,
// and returns a Client ready for mac_quotation protocols (0x122C, etc.).
func DialSP(addr string, opts ...Option) (*Client, error) {
	c, err := Dial(addr, opts...)
	if err != nil {
		return nil, err
	}

	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	if err := c.spLogin(cfg.timeout); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("tdx: sp login %s: %w", addr, err)
	}

	c.spMode = true
	return c, nil
}

// Close closes the underlying TCP connection and waits for the listener to exit.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.mu.Unlock()

	<-c.done
	return nil
}

// Addr returns the server address this client is connected to.
func (c *Client) Addr() string {
	return c.addr
}

// ---------------------------------------------------------------------------
// Internal: bootstrap handshake
// ---------------------------------------------------------------------------

func (c *Client) bootstrap(timeout time.Duration) error {
	// Stage 1: Ping (0x0015)
	if err := c.sendRaw(PingRequest(c.nextSeq())); err != nil {
		return fmt.Errorf("send ping: %w", err)
	}
	if _, err := c.waitFor(ServiceIDPing, PingCMD, timeout); err != nil {
		return fmt.Errorf("wait ping: %w", err)
	}

	// Stage 2: ConnectAuth (0x1894/0x000D)
	if err := c.sendRaw(ConnectAuthRequest(c.nextSeq())); err != nil {
		return fmt.Errorf("send connect-auth: %w", err)
	}
	if _, err := c.waitFor(ServiceIDAuth, 0x000D, timeout); err != nil {
		return fmt.Errorf("wait connect-auth: %w", err)
	}

	// Stage 3: Stage2 (0x1899/0x0FDB)
	if err := c.sendRaw(Stage2Request(c.nextSeq())); err != nil {
		return fmt.Errorf("send stage2: %w", err)
	}
	if _, err := c.waitFor(ServiceIDStage, Stage2CMD, timeout); err != nil {
		return fmt.Errorf("wait stage2: %w", err)
	}

	c.bootstrapped = true
	return nil
}

func (c *Client) spLogin(timeout time.Duration) error {
	c.drainPending()
	if err := c.sendRaw(SPLoginRequest(c.nextSeq())); err != nil {
		return fmt.Errorf("send sp login: %w", err)
	}
	if _, err := c.waitForCMD(SPLoginCMD, timeout); err != nil {
		return fmt.Errorf("wait sp login: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal: frame listener
// ---------------------------------------------------------------------------

func (c *Client) listen() {
	defer close(c.done)
	reader := bufio.NewReader(c.conn)
	for {
		magic, err := reader.Peek(4)
		if err != nil {
			return
		}

		// Response frame: 0xB1CB7400
		if bytes.Equal(magic, []byte{0xb1, 0xcb, 0x74, 0x00}) {
			headerData := make([]byte, 16)
			if _, err := io.ReadFull(reader, headerData); err != nil {
				return
			}
			header, err := DecodeResponseHeader(headerData)
			if err != nil {
				return
			}
			payloadLen := int(header.ZipLen)
			if payloadLen == 0 {
				payloadLen = int(header.RawLen)
			}
			payload := make([]byte, payloadLen)
			if payloadLen > 0 {
				if _, err := io.ReadFull(reader, payload); err != nil {
					return
				}
			}
			packet, err := DecodeResponsePacket(headerData, payload)
			if err != nil {
				return
			}
			select {
			case c.packetCh <- packet:
			default:
			}
			continue
		}

		// Direct frame: 0x0C or 0x0B
		if magic[0] == 0x0C || magic[0] == 0x0B {
			headerPeek, _ := reader.Peek(12)
			if len(headerPeek) < 12 {
				return
			}
			len1 := binary.LittleEndian.Uint16(headerPeek[8:10])
			if len1 < 2 {
				reader.Discard(12)
				continue
			}
			frame := make([]byte, 12+int(len1))
			if _, err := io.ReadFull(reader, frame); err != nil {
				return
			}
			if len(frame) >= 12 {
				packet := &ResponsePacket{
					Header: &ResponseHeader{
						Magic:  0xB1CB7400,
						CMD:    binary.LittleEndian.Uint16(frame[10:12]),
						ZipLen: 0,
						RawLen: uint16(len(frame) - 12),
					},
					Body: &DecodedBody{
						WirePayload: frame[12:],
						Decoded:     frame[12:],
					},
				}
				select {
				case c.packetCh <- packet:
				default:
				}
			}
			continue
		}

		// SP mode frame: 0x01 (mac_quotation protocols)
		// Wire: [head:1][customize:4][control:1][len:2][len:2][msg_id:2][body...]
		if magic[0] == 0x01 {
			headerPeek, _ := reader.Peek(10)
			if len(headerPeek) < 10 {
				return
			}
			bodyLen := binary.LittleEndian.Uint16(headerPeek[6:8])
			if bodyLen < 2 {
				reader.Discard(10)
				continue
			}
			frame := make([]byte, 10+int(bodyLen))
			if _, err := io.ReadFull(reader, frame); err != nil {
				return
			}
			msgID := binary.LittleEndian.Uint16(frame[10:12])
			packet := &ResponsePacket{
				Header: &ResponseHeader{
					Magic:  0xB1CB7400,
					CMD:    msgID,
					ZipLen: 0,
					RawLen: uint16(bodyLen - 2),
				},
				Body: &DecodedBody{
					WirePayload: frame[12:],
					Decoded:     frame[12:],
				},
			}
			select {
			case c.packetCh <- packet:
			default:
			}
			continue
		}

		// Unknown byte, skip
		reader.Discard(1)
	}
}

// ---------------------------------------------------------------------------
// Internal: send / receive
// ---------------------------------------------------------------------------

func (c *Client) sendRaw(packet []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.conn == nil {
		return fmt.Errorf("client is closed")
	}
	_, err := c.conn.Write(packet)
	return err
}

func (c *Client) nextSeq() uint16 {
	seq := uint16(c.pktSeq & 0xFFFF)
	c.pktSeq++
	return seq
}

func (c *Client) waitFor(serviceID, cmd uint16, timeout time.Duration) (*ResponsePacket, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case pkt := <-c.packetCh:
			if pkt == nil || pkt.Header == nil {
				continue
			}
			if pkt.Header.ServiceID == serviceID && pkt.Header.CMD == cmd {
				return pkt, nil
			}
			if serviceID == 0 && pkt.Header.CMD == cmd {
				return pkt, nil
			}
		case <-deadline.C:
			return nil, fmt.Errorf("timeout waiting service=0x%04X cmd=0x%04X", serviceID, cmd)
		}
	}
}

// waitForCMD waits for a response matching the given CMD (ignoring serviceID).
func (c *Client) waitForCMD(cmd uint16, timeout time.Duration) (*ResponsePacket, error) {
	return c.waitFor(0, cmd, timeout)
}

// drainPending discards all pending response packets.
func (c *Client) drainPending() {
	for {
		select {
		case <-c.packetCh:
		default:
			return
		}
	}
}

// ensureConn returns error if client is not connected.
func (c *Client) ensureConn() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("tdx: client is closed")
	}
	if c.conn == nil {
		return fmt.Errorf("tdx: client is not connected")
	}
	return nil
}
