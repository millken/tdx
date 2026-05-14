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

type clientConn struct {
	mu           sync.Mutex
	reqMu        sync.Mutex // serializes drain+send+wait round-trips against concurrent callers
	conn         net.Conn
	addr         string
	closed       bool
	bootstrapped bool
	timeout      time.Duration
	pktSeq       uint32
	packetCh     chan *ResponsePacket
	done         chan struct{}
}

// MainClient represents a connection to a TDX 7709 market data server.
type MainClient struct {
	*clientConn
}

// ExClient represents a connection to a TDX 7727 extension quote server.
type ExClient struct {
	*clientConn
}

// Client keeps the main-quote client name in this module.
type Client = MainClient

func newClientConn(conn net.Conn, addr string, timeout time.Duration) *clientConn {
	return &clientConn{
		conn:     conn,
		addr:     addr,
		timeout:  timeout,
		packetCh: make(chan *ResponsePacket, 32),
		done:     make(chan struct{}),
	}
}

// Dial connects to a TDX server, performs the 3-stage bootstrap handshake
// (Ping → ConnectAuth → Stage2), and returns a ready-to-use Client.
func Dial(addr string, opts ...Option) (*MainClient, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	conn, err := net.DialTimeout("tcp", addr, cfg.timeout)
	if err != nil {
		return nil, fmt.Errorf("tdx: dial %s: %w", addr, err)
	}

	c := &MainClient{clientConn: newClientConn(conn, addr, cfg.timeout)}

	go c.listen()

	if err := c.bootstrap(cfg.timeout); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("tdx: bootstrap %s: %w", addr, err)
	}

	return c, nil
}

// DialSP connects to a TDX mac_hosts server, performs standard bootstrap + SP mode login,
// and returns a Client ready for mac_quotation protocols (0x122C, etc.).
func DialSP(addr string, opts ...Option) (*MainClient, error) {
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

	return c, nil
}

// DialEx connects to a 7727 extension quote server, performs SP login +
// extension bootstrap, and returns a Client ready for extension quote requests.
func DialEx(addr string, opts ...Option) (*ExClient, error) {
	return DialFund(addr, opts...)
}

// DialFund connects to a 7727 extension quote server, performs SP login + fund bootstrap,
// and returns a Client ready for fund KLine requests.
func DialFund(addr string, opts ...Option) (*ExClient, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	conn, err := net.DialTimeout("tcp", addr, cfg.timeout)
	if err != nil {
		return nil, fmt.Errorf("tdx: dial %s: %w", addr, err)
	}

	c := &ExClient{clientConn: newClientConn(conn, addr, cfg.timeout)}

	go c.listen()

	if err := c.spLogin(cfg.timeout); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("tdx: sp login %s: %w", addr, err)
	}
	if err := c.fundBootstrap(cfg.timeout); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("tdx: fund bootstrap %s: %w", addr, err)
	}

	return c, nil
}

// DialBest probes addresses (sorted by latency), then tries each with Dial
// until one succeeds. Uses in-memory cache with 10min TTL.
func DialBest(addresses []string, opts ...Option) (*MainClient, error) {
	if len(addresses) == 0 {
		addresses = BestMainAddresses(0)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("tdx: no addresses to dial")
	}
	var lastErr error
	for _, addr := range addresses {
		c, err := Dial(addr, opts...)
		if err != nil {
			lastErr = err
			continue
		}
		return c, nil
	}
	return nil, lastErr
}

// DialSPBest probes addresses, then tries each with DialSP until one succeeds.
func DialSPBest(addresses []string, opts ...Option) (*MainClient, error) {
	if len(addresses) == 0 {
		addresses = BestSPAddresses(0)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("tdx: no SP addresses to dial")
	}
	var lastErr error
	for _, addr := range addresses {
		c, err := DialSP(addr, opts...)
		if err != nil {
			lastErr = err
			continue
		}
		return c, nil
	}
	return nil, lastErr
}

// DialExBest probes extension quote addresses, then tries each with DialEx until one succeeds.
func DialExBest(addresses []string, opts ...Option) (*ExClient, error) {
	if len(addresses) == 0 {
		addresses = BestExAddresses(0)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("tdx: no fund addresses to dial")
	}
	var lastErr error
	for _, addr := range addresses {
		c, err := DialFund(addr, opts...)
		if err != nil {
			lastErr = err
			continue
		}
		return c, nil
	}
	return nil, lastErr
}

// Close closes the underlying TCP connection and waits for the listener to exit.
func (c *clientConn) Close() error {
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
func (c *clientConn) Addr() string {
	return c.addr
}

// IsAlive reports whether the underlying connection is still active.
// It checks the closed flag and the done channel set by the listen goroutine.
// A TCP-level peek is intentionally avoided: setting a read deadline on the
// shared net.Conn races with the listen() goroutine's bufio.Reader and would
// cause the listener to see a spurious timeout and exit.
func (c *clientConn) IsAlive() bool {
	select {
	case <-c.done:
		return false
	default:
	}
	c.mu.Lock()
	closed := c.closed || c.conn == nil
	c.mu.Unlock()
	return !closed
}

// ---------------------------------------------------------------------------
// Internal: bootstrap handshake
// ---------------------------------------------------------------------------

func (c *clientConn) bootstrap(timeout time.Duration) error {
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

func (c *clientConn) spLogin(timeout time.Duration) error {
	c.drainPending()
	if err := c.sendRaw(SPLoginRequest(c.nextSeq())); err != nil {
		return fmt.Errorf("send sp login: %w", err)
	}
	if _, err := c.waitForCMD(SPLoginCMD, timeout); err != nil {
		return fmt.Errorf("wait sp login: %w", err)
	}
	return nil
}

func (c *clientConn) fundBootstrap(timeout time.Duration) error {
	c.drainPending()
	if err := c.sendRaw(FundBootstrapRequest(c.nextSeq())); err != nil {
		return fmt.Errorf("send fund bootstrap: %w", err)
	}
	if _, err := c.waitForCMD(FundBootstrapCMD, timeout); err != nil {
		return fmt.Errorf("wait fund bootstrap: %w", err)
	}
	c.bootstrapped = true
	return nil
}

// ---------------------------------------------------------------------------
// Internal: frame listener
// ---------------------------------------------------------------------------

func (c *clientConn) listen() {
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

func (c *clientConn) sendRaw(packet []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.conn == nil {
		return fmt.Errorf("client is closed")
	}
	_, err := c.conn.Write(packet)
	return err
}

func (c *clientConn) nextSeq() uint16 {
	seq := uint16(c.pktSeq & 0xFFFF)
	c.pktSeq++
	return seq
}

func (c *clientConn) waitFor(serviceID, cmd uint16, timeout time.Duration) (*ResponsePacket, error) {
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
		case <-c.done:
			return nil, fmt.Errorf("connection closed waiting cmd=0x%04X", cmd)
		case <-deadline.C:
			return nil, fmt.Errorf("timeout waiting service=0x%04X cmd=0x%04X", serviceID, cmd)
		}
	}
}

// waitForCMD waits for a response matching the given CMD (ignoring serviceID).
func (c *clientConn) waitForCMD(cmd uint16, timeout time.Duration) (*ResponsePacket, error) {
	return c.waitFor(0, cmd, timeout)
}

// drainPending discards all pending response packets.
func (c *clientConn) drainPending() {
	for {
		select {
		case <-c.packetCh:
		default:
			return
		}
	}
}

// do performs a single request/response round-trip serialized against
// other concurrent users of the same connection. The shared response channel
// makes interleaved drainPending/sendRaw/waitForCMD calls unsafe; this helper
// holds reqMu for the entire roundtrip.
func (c *clientConn) do(packet []byte, cmd uint16, timeout time.Duration) (*ResponsePacket, error) {
	c.reqMu.Lock()
	defer c.reqMu.Unlock()
	c.drainPending()
	if err := c.sendRaw(packet); err != nil {
		return nil, err
	}
	return c.waitForCMD(cmd, timeout)
}

// ensureConn returns error if client is not connected.
func (c *clientConn) ensureConn() error {
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
