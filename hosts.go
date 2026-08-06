package tdx

import (
	"errors"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// HostInfo describes a TDX server entry.
type HostInfo struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

// Address returns the "host:port" form.
func (h HostInfo) Address() string {
	return net.JoinHostPort(h.IP, strconv.Itoa(h.Port))
}

// HostProbeResult records a single TCP probe result.
type HostProbeResult struct {
	Name      string        `json:"name"`
	IP        string        `json:"ip"`
	Port      int           `json:"port"`
	Address   string        `json:"address"`
	Latency   time.Duration `json:"latency"`
	Reachable bool          `json:"reachable"`
	Error     string        `json:"error,omitempty"`
}

// ErrNoReachableHosts indicates that every probed host failed.
var ErrNoReachableHosts = errors.New("no reachable hosts")

// ---------------------------------------------------------------------------
// Built-in server lists (from gotdx)
// ---------------------------------------------------------------------------

var mainHosts = []HostInfo{
	{Name: "通达信深圳双线主站1", IP: "110.41.147.114", Port: 7709},
	{Name: "通达信深圳双线主站2", IP: "110.41.2.72", Port: 7709},
	{Name: "通达信深圳双线主站3", IP: "110.41.4.4", Port: 7709},
	{Name: "通达信深圳双线主站4", IP: "47.113.94.204", Port: 7709},
	{Name: "通达信深圳双线主站5", IP: "8.129.174.169", Port: 7709},
	{Name: "通达信深圳双线主站6", IP: "110.41.154.219", Port: 7709},
	{Name: "通达信上海双线主站1", IP: "124.70.176.52", Port: 7709},
	{Name: "通达信上海双线主站2", IP: "47.100.236.28", Port: 7709},
	{Name: "通达信上海双线主站3", IP: "123.60.186.45", Port: 7709},
	{Name: "通达信上海双线主站4", IP: "123.60.164.122", Port: 7709},
	{Name: "通达信上海双线主站5", IP: "47.116.105.28", Port: 7709},
	{Name: "通达信上海双线主站6", IP: "124.70.199.56", Port: 7709},
	{Name: "通达信北京双线主站1", IP: "121.36.54.217", Port: 7709},
	{Name: "通达信北京双线主站2", IP: "121.36.81.195", Port: 7709},
	{Name: "通达信北京双线主站3", IP: "123.249.15.60", Port: 7709},
	{Name: "通达信广州双线主站1", IP: "124.71.85.110", Port: 7709},
	{Name: "通达信广州双线主站2", IP: "139.9.51.18", Port: 7709},
	{Name: "通达信广州双线主站3", IP: "139.159.239.163", Port: 7709},
	{Name: "通达信上海双线主站7", IP: "106.14.201.131", Port: 7709},
	{Name: "通达信上海双线主站8", IP: "106.14.190.242", Port: 7709},
	{Name: "通达信上海双线主站9", IP: "121.36.225.169", Port: 7709},
	{Name: "通达信上海双线主站10", IP: "123.60.70.228", Port: 7709},
	{Name: "通达信上海双线主站11", IP: "123.60.73.44", Port: 7709},
	{Name: "通达信上海双线主站12", IP: "124.70.133.119", Port: 7709},
	{Name: "通达信上海双线主站13", IP: "124.71.187.72", Port: 7709},
	{Name: "通达信上海双线主站14", IP: "124.71.187.122", Port: 7709},
	{Name: "通达信武汉电信主站1", IP: "119.97.185.59", Port: 7709},
	{Name: "通达信深圳双线主站7", IP: "47.107.64.168", Port: 7709},
	{Name: "通达信北京双线主站4", IP: "124.70.75.113", Port: 7709},
	{Name: "通达信广州双线主站4", IP: "124.71.9.153", Port: 7709},
	{Name: "通达信上海双线主站15", IP: "123.60.84.66", Port: 7709},
	{Name: "通达信深圳双线主站8", IP: "47.107.228.47", Port: 7719},
	{Name: "通达信北京双线主站5", IP: "120.46.186.223", Port: 7709},
	{Name: "通达信北京双线主站6", IP: "124.70.22.210", Port: 7709},
	{Name: "通达信北京双线主站7", IP: "139.9.133.247", Port: 7709},
	{Name: "通达信广州双线主站5", IP: "116.205.163.254", Port: 7709},
	{Name: "通达信广州双线主站6", IP: "116.205.171.132", Port: 7709},
	{Name: "通达信广州双线主站7", IP: "116.205.183.150", Port: 7709},
}

var macHosts = []HostInfo{
	{Name: "行情主站1", IP: "121.36.248.138", Port: 7709},
	{Name: "行情主站2", IP: "123.60.47.136", Port: 7709},
	{Name: "行情主站3", IP: "121.37.207.165", Port: 7709},
}

var fundHosts = []HostInfo{
	{Name: "基金扩展主站1", IP: "112.74.214.43", Port: 7727},
	{Name: "基金扩展主站2", IP: "120.25.218.6", Port: 7727},
	{Name: "基金扩展主站3", IP: "47.107.75.159", Port: 7727},
	{Name: "基金扩展主站4", IP: "47.106.204.218", Port: 7727},
	{Name: "基金扩展主站5", IP: "47.106.209.131", Port: 7727},
	{Name: "基金扩展主站6", IP: "139.9.191.175", Port: 7727},
	{Name: "基金扩展主站7", IP: "47.115.94.72", Port: 7727},
	{Name: "基金扩展主站8", IP: "106.14.95.149", Port: 7727},
	{Name: "基金扩展主站9", IP: "47.102.108.214", Port: 7727},
	{Name: "基金扩展主站10", IP: "47.103.86.229", Port: 7727},
	{Name: "基金扩展主站11", IP: "47.103.88.146", Port: 7727},
	{Name: "基金扩展主站12", IP: "116.205.143.214", Port: 7727},
	{Name: "基金扩展主站13", IP: "124.71.223.19", Port: 7727},
}

// icfqsHosts 是 ICFQS TQLEX HTTP 服务地址列表 (题材/龙虎榜等云端数据)。
var icfqsHosts = []HostInfo{
	{Name: "华为均衡上海1", IP: "119.3.157.89", Port: 7615},
	{Name: "华为均衡广州1", IP: "139.9.211.159", Port: 7615},
	{Name: "华为均衡广州2", IP: "121.37.193.4", Port: 7615},
	{Name: "华为均衡广州3", IP: "124.71.56.161", Port: 7615},
	{Name: "华为均衡上海2", IP: "123.60.69.160", Port: 7615},
	{Name: "华为均衡上海3", IP: "123.60.149.213", Port: 7615},
	{Name: "腾讯上海均衡1", IP: "118.25.106.154", Port: 7615},
	{Name: "腾讯广州均衡1", IP: "129.204.254.13", Port: 7615},
	{Name: "腾讯广州均衡2", IP: "159.75.115.35", Port: 7615},
	{Name: "腾讯北京均衡", IP: "82.157.190.225", Port: 7615},
	{Name: "华为均衡5", IP: "121.36.192.253", Port: 7615},
	{Name: "华为广州均衡5", IP: "124.71.105.217", Port: 7615},
}

// MainHosts returns the built-in main quote servers.
func MainHosts() []HostInfo { return append([]HostInfo(nil), mainHosts...) }

// SPHosts returns the built-in MAC/SP quote servers.
func SPHosts() []HostInfo { return append([]HostInfo(nil), macHosts...) }

// ExHosts returns the built-in extension quote servers.
func ExHosts() []HostInfo { return FundHosts() }

// FundHosts returns the built-in fund quote servers.
func FundHosts() []HostInfo { return append([]HostInfo(nil), fundHosts...) }

// ICFQSHosts returns the built-in ICFQS TQLEX HTTP servers.
func ICFQSHosts() []HostInfo { return append([]HostInfo(nil), icfqsHosts...) }

// MainAddresses returns the built-in main quote host:port list.
func MainAddresses() []string { return hostAddresses(mainHosts) }

// SPAddresses returns the built-in MAC/SP host:port list.
func SPAddresses() []string { return hostAddresses(macHosts) }

// ExAddresses returns the built-in extension quote host:port list.
func ExAddresses() []string { return FundAddresses() }

// FundAddresses returns the built-in fund host:port list.
func FundAddresses() []string { return hostAddresses(fundHosts) }

// ICFQSAddresses returns the built-in ICFQS TQLEX host:port list.
func ICFQSAddresses() []string { return hostAddresses(icfqsHosts) }

func hostAddresses(hosts []HostInfo) []string {
	addrs := make([]string, 0, len(hosts))
	for _, h := range hosts {
		addrs = append(addrs, h.Address())
	}
	return addrs
}

// ---------------------------------------------------------------------------
// Probe (no cache)
// ---------------------------------------------------------------------------

// ProbeAddresses concurrently dials each address and returns results sorted
// by reachability then latency (ascending).
func ProbeAddresses(addresses []string, timeout time.Duration) []HostProbeResult {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	results := make([]HostProbeResult, 0, len(addresses))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, addr := range addresses {
		addr := addr
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := parseAddress(addr)
			start := time.Now()
			conn, err := net.DialTimeout("tcp", addr, timeout)
			if err != nil {
				r.Error = err.Error()
			} else {
				r.Reachable = true
				r.Latency = time.Since(start)
				_ = conn.Close()
			}
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
		}()
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool {
		if results[i].Reachable != results[j].Reachable {
			return results[i].Reachable
		}
		if results[i].Reachable && results[j].Reachable && results[i].Latency != results[j].Latency {
			return results[i].Latency < results[j].Latency
		}
		return results[i].Address < results[j].Address
	})
	return results
}

// ProbeHosts probes HostInfo entries and enriches results with Name/IP/Port.
func ProbeHosts(hosts []HostInfo, timeout time.Duration) []HostProbeResult {
	results := ProbeAddresses(hostAddresses(hosts), timeout)
	nameMap := make(map[string]HostInfo, len(hosts))
	for _, h := range hosts {
		nameMap[h.Address()] = h
	}
	for i := range results {
		if h, ok := nameMap[results[i].Address]; ok {
			results[i].Name = h.Name
			results[i].IP = h.IP
			results[i].Port = h.Port
		}
	}
	return results
}

// FastestAddress returns the single fastest reachable address.
func FastestAddress(addresses []string, timeout time.Duration) (HostProbeResult, error) {
	for _, r := range ProbeAddresses(addresses, timeout) {
		if r.Reachable {
			return r, nil
		}
	}
	return HostProbeResult{}, ErrNoReachableHosts
}

func parseAddress(addr string) HostProbeResult {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return HostProbeResult{Address: addr, Error: err.Error()}
	}
	p, _ := strconv.Atoi(port)
	return HostProbeResult{IP: host, Port: p, Address: addr}
}

// ---------------------------------------------------------------------------
// Cached probe (in-memory, TTL 10min)
// ---------------------------------------------------------------------------

type probeCache struct {
	ts      time.Time
	results []HostProbeResult
}

var (
	mainCacheMu sync.RWMutex
	mainCache   probeCache
	spCacheMu   sync.RWMutex
	spCache     probeCache
	fundCacheMu sync.RWMutex
	fundCache   probeCache
)

const cacheTTL = 10 * time.Minute

// BestMainAddresses returns reachable main addresses sorted by latency.
// Results are cached in-memory for 10 minutes.
func BestMainAddresses(timeout time.Duration) []string {
	mainCacheMu.RLock()
	if time.Since(mainCache.ts) < cacheTTL && len(mainCache.results) > 0 {
		addrs := reachableAddresses(mainCache.results)
		mainCacheMu.RUnlock()
		return addrs
	}
	mainCacheMu.RUnlock()

	mainCacheMu.Lock()
	defer mainCacheMu.Unlock()
	// double-check
	if time.Since(mainCache.ts) < cacheTTL && len(mainCache.results) > 0 {
		return reachableAddresses(mainCache.results)
	}
	mainCache.ts = time.Now()
	mainCache.results = ProbeHosts(mainHosts, timeout)
	return reachableAddresses(mainCache.results)
}

// BestSPAddresses returns reachable SP addresses sorted by latency.
// Results are cached in-memory for 10 minutes.
func BestSPAddresses(timeout time.Duration) []string {
	spCacheMu.RLock()
	if time.Since(spCache.ts) < cacheTTL && len(spCache.results) > 0 {
		addrs := reachableAddresses(spCache.results)
		spCacheMu.RUnlock()
		return addrs
	}
	spCacheMu.RUnlock()

	spCacheMu.Lock()
	defer spCacheMu.Unlock()
	if time.Since(spCache.ts) < cacheTTL && len(spCache.results) > 0 {
		return reachableAddresses(spCache.results)
	}
	spCache.ts = time.Now()
	spCache.results = ProbeHosts(macHosts, timeout)
	return reachableAddresses(spCache.results)
}

// BestExAddresses returns reachable extension quote addresses sorted by latency.
func BestExAddresses(timeout time.Duration) []string {
	fundCacheMu.RLock()
	if time.Since(fundCache.ts) < cacheTTL && len(fundCache.results) > 0 {
		addrs := reachableAddresses(fundCache.results)
		fundCacheMu.RUnlock()
		return addrs
	}
	fundCacheMu.RUnlock()

	fundCacheMu.Lock()
	defer fundCacheMu.Unlock()
	if time.Since(fundCache.ts) < cacheTTL && len(fundCache.results) > 0 {
		return reachableAddresses(fundCache.results)
	}
	fundCache.ts = time.Now()
	fundCache.results = ProbeHosts(fundHosts, timeout)
	return reachableAddresses(fundCache.results)
}

// CachedMainResults returns the cached main probe results (for display).
func CachedMainResults(timeout time.Duration) []HostProbeResult {
	BestMainAddresses(timeout) // ensure populated
	mainCacheMu.RLock()
	defer mainCacheMu.RUnlock()
	return append([]HostProbeResult(nil), mainCache.results...)
}

// CachedSPResults returns the cached SP probe results (for display).
func CachedSPResults(timeout time.Duration) []HostProbeResult {
	BestSPAddresses(timeout) // ensure populated
	spCacheMu.RLock()
	defer spCacheMu.RUnlock()
	return append([]HostProbeResult(nil), spCache.results...)
}

func reachableAddresses(results []HostProbeResult) []string {
	addrs := make([]string, 0, len(results))
	for _, r := range results {
		if r.Reachable {
			addrs = append(addrs, r.Address)
		}
	}
	return addrs
}
