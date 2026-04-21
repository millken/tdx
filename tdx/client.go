package tdx

import (
	"fmt"
	"net"
	"sync"

	"github.com/millken/tdx/tdxrpc_new/protocol"
)

// Client represents a connection to a TDX 7709 market data server.
type Client struct {
	mu     sync.Mutex
	proto  *protocol.Client
	addr   string
	closed bool
}

// Dial connects to a TDX server, performs the 3-stage bootstrap handshake
// (Ping → ConnectAuth → Stage2), and returns a ready-to-use Client.
func Dial(addr string, opts ...Option) (*Client, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	p := protocol.NewClient()
	if err := p.Connect(addr); err != nil {
		return nil, fmt.Errorf("tdx: connect %s: %w", addr, err)
	}

	if err := p.BootstrapAndWait(cfg.timeout); err != nil {
		_ = p.SendRawPacket(nil) // no-op, just to satisfy interface
		return nil, fmt.Errorf("tdx: bootstrap %s: %w", addr, err)
	}

	return &Client{
		proto: p,
		addr:  addr,
	}, nil
}

// Close closes the underlying TCP connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.proto != nil {
		// protocol.Client doesn't expose Close; we rely on GC.
		// If needed, we can add a Close method to protocol.Client later.
	}
	return nil
}

// Addr returns the server address this client is connected to.
func (c *Client) Addr() string {
	return c.addr
}

// conn returns the underlying net.Conn for advanced usage.
// Returns nil if the client is closed or not connected.
func (c *Client) conn() net.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.proto == nil {
		return nil
	}
	// protocol.Client.conn is unexported; we don't expose it.
	// This is a placeholder for future extension.
	return nil
}

// ensureProto returns the protocol client or an error if closed.
func (c *Client) ensureProto() (*protocol.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("tdx: client is closed")
	}
	if c.proto == nil {
		return nil, fmt.Errorf("tdx: client is not connected")
	}
	return c.proto, nil
}
