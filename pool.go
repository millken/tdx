package tdx

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Pool maintains a set of connections to the fastest TDX servers.
// It automatically probes nodes, establishes connections, and runs
// background health checks to replace dead connections.
type Pool struct {
	mu      sync.RWMutex
	clients []*clientConn
	addrs   []string // candidate addresses sorted by latency
	sp      bool
	ex      bool
	opts    []Option
	size    int
	index   uint64 // round-robin counter
	done    chan struct{}
	closeMu sync.Mutex
	closed  bool
}

// PoolStats holds pool status information.
type PoolStats struct {
	Size   int // desired pool size
	Active int // currently alive connections
}

// NewPool creates a connection pool of the given size.
// It probes servers, connects to the fastest ones, and starts
// a background maintenance goroutine.
func NewPool(size int, opts ...Option) (*Pool, error) {
	if size <= 0 {
		return nil, fmt.Errorf("tdx: pool size must be > 0, got %d", size)
	}

	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	var addrs []string
	if cfg.ex {
		addrs = BestExAddresses(0)
	} else if cfg.sp {
		addrs = BestSPAddresses(0)
	} else {
		addrs = BestMainAddresses(0)
	}

	p := &Pool{
		addrs: addrs,
		sp:    cfg.sp,
		ex:    cfg.ex,
		opts:  opts,
		size:  size,
		done:  make(chan struct{}),
	}

	p.fill()

	if len(p.clients) == 0 {
		return nil, fmt.Errorf("tdx: pool: no reachable hosts")
	}

	go p.maintain()
	return p, nil
}

// Get returns a client using round-robin. If the selected client is dead,
// it is replaced with a new connection before returning.
func (p *Pool) Get() *MainClient {
	if p.ex {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.clients)
	if n == 0 {
		c := p.dialAny()
		if c != nil {
			p.clients = append(p.clients, c)
		}
		if c == nil {
			return nil
		}
		return &MainClient{clientConn: c}
	}

	idx := atomic.AddUint64(&p.index, 1) - 1
	c := p.clients[int(idx)%n]

	if !c.IsAlive() {
		_ = c.Close()
		replacement := p.dialAny()
		if replacement != nil {
			p.clients[int(idx)%n] = replacement
			return &MainClient{clientConn: replacement}
		}
	}

	return &MainClient{clientConn: c}
}

// GetEx returns an extension quote client using round-robin.
// For non-Ex pools, it returns nil.
func (p *Pool) GetEx() *ExClient {
	if !p.ex {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.clients)
	if n == 0 {
		c := p.dialAny()
		if c != nil {
			p.clients = append(p.clients, c)
		}
		if c == nil {
			return nil
		}
		return &ExClient{clientConn: c}
	}

	idx := atomic.AddUint64(&p.index, 1) - 1
	c := p.clients[int(idx)%n]

	if !c.IsAlive() {
		_ = c.Close()
		replacement := p.dialAny()
		if replacement != nil {
			p.clients[int(idx)%n] = replacement
			return &ExClient{clientConn: replacement}
		}
	}

	return &ExClient{clientConn: c}
}

// Stats returns current pool statistics.
func (p *Pool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	active := 0
	for _, c := range p.clients {
		if c.IsAlive() {
			active++
		}
	}
	return PoolStats{Size: p.size, Active: active}
}

// Close shuts down the background goroutine and closes all connections.
func (p *Pool) Close() error {
	p.closeMu.Lock()
	if p.closed {
		p.closeMu.Unlock()
		return nil
	}
	p.closed = true
	close(p.done)
	p.closeMu.Unlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.clients {
		_ = c.Close()
	}
	p.clients = nil
	return nil
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (p *Pool) fill() {
	p.mu.Lock()
	defer p.mu.Unlock()

	alive := 0
	for _, c := range p.clients {
		if c.IsAlive() {
			alive++
		}
	}

	need := p.size - alive
	if need <= 0 {
		return
	}

	usedAddrs := make(map[string]bool, len(p.clients))
	for _, c := range p.clients {
		if c.IsAlive() {
			usedAddrs[c.Addr()] = true
		}
	}

	type result struct {
		client *clientConn
	}
	ch := make(chan result, need*2)
	launched := 0

	for _, addr := range p.addrs {
		if launched >= need*2 { // try up to 2x candidates
			break
		}
		if usedAddrs[addr] {
			continue
		}

		addr := addr
		launched++
		go func() {
			var c *clientConn
			var err error
			if p.ex {
				var exClient *ExClient
				exClient, err = DialEx(addr, p.opts...)
				if err == nil {
					c = exClient.clientConn
				}
			} else if p.sp {
				var mainClient *MainClient
				mainClient, err = DialSP(addr, p.opts...)
				if err == nil {
					c = mainClient.clientConn
				}
			} else {
				var mainClient *MainClient
				mainClient, err = Dial(addr, p.opts...)
				if err == nil {
					c = mainClient.clientConn
				}
			}
			if err != nil {
				ch <- result{}
				return
			}
			ch <- result{client: c}
		}()
	}

	for i := 0; i < launched; i++ {
		r := <-ch
		if r.client != nil && len(p.clients) < p.size {
			p.clients = append(p.clients, r.client)
		} else if r.client != nil {
			_ = r.client.Close()
		}
	}
}

func (p *Pool) dialAny() *clientConn {
	for _, addr := range p.addrs {
		var err error
		if p.ex {
			exClient, dialErr := DialEx(addr, p.opts...)
			if dialErr == nil {
				return exClient.clientConn
			}
			err = dialErr
		} else if p.sp {
			mainClient, dialErr := DialSP(addr, p.opts...)
			if dialErr == nil {
				return mainClient.clientConn
			}
			err = dialErr
		} else {
			mainClient, dialErr := Dial(addr, p.opts...)
			if dialErr == nil {
				return mainClient.clientConn
			}
			err = dialErr
		}
		_ = err
	}
	return nil
}

func (p *Pool) maintain() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.replaceDead()
			p.fill()
		}
	}
}

func (p *Pool) replaceDead() {
	p.mu.Lock()
	defer p.mu.Unlock()

	alive := make([]*clientConn, 0, len(p.clients))
	for _, c := range p.clients {
		if c.IsAlive() {
			alive = append(alive, c)
		} else {
			_ = c.Close()
		}
	}
	if len(alive) != len(p.clients) {
		p.clients = alive
	}
}
