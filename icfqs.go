package tdx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ICFQS 是通达信云端 TQLEX HTTP 接口客户端，提供主站二进制协议无法获取的
// 云端数据：题材/概念板块、龙虎榜、营业部统计、每日必看复盘等。
//
// 接口形态：POST http://<host>:7615/TQLEX?Entry=<entry>，请求体为 JSON。
// 响应分两种：
//   - TQLEX 协议 (Entry=CWServ.*/DataAggregation.*)：响应体是「外层包装文本 +
//     内层 JSON 对象」，由 parseICFQSTQLResponse 抽取内层 JSON。
//   - 标准 JSON (Entry=HQServ.*)：响应体直接是 JSON。
//
// 所有 Raw 方法返回未加工的 map[string]any，字段含义由 entry 决定。可调用
// ICFQSFormatTables 将 ResultSets 转为按列名索引的表格视图。

const (
	// DefaultICFQSAddress 是默认 ICFQS TQLEX 服务地址 (华为均衡广州2)。
	DefaultICFQSAddress = "121.37.193.4:7615"
	// DefaultICFQSHotAddress 是热门题材专用地址。
	DefaultICFQSHotAddress = "hot.icfqs.com:7615"
)

// ICFQSOption 配置 ICFQSClient。
type ICFQSOption func(*ICFQSClient)

// ICFQSClient 是 ICFQS TQLEX HTTP 客户端。
//
// 云端 entry 分两组，分别只在特定域名开放：
//   - 题材/概念类 (ph_tdxdatacenter_*、DataAggregation.*) → 普通均衡节点 (address)
//   - 龙虎榜/复盘类 (cfg_fx_*、cfg_tk_*) → hot.icfqs.com (hotAddress)
//
// 客户端同时持有两个地址，业务方法按所属组直接选用，不做试错重试。
type ICFQSClient struct {
	address     string        // 普通均衡节点 (题材类)
	hotAddress  string        // hot.icfqs.com (龙虎榜/复盘类)
	httpClient  *http.Client // 底层 HTTP 客户端
}

// ICFQSCode 标识一个市场/代码对，用于批量行情查询。
type ICFQSCode struct {
	Setcode string `json:"setcode"` // 市场代码 (沪=1 深=0)
	Code    string `json:"code"`    // 6 位证券代码
}

// ICFQSTable 是 TQLEX ResultSets 项的表格视图，按列名索引每行。
type ICFQSTable struct {
	Rows []map[string]any `json:"rows"`
}

// NewICFQS 创建一个 ICFQS HTTP 客户端。
//
// 同时持有两个地址：address (普通均衡节点，题材类 entry) 和 hotAddress (hot.icfqs.com，
// 龙虎榜/复盘类 cfg_* entry)。业务方法按 entry 所属组静态选路，不做试错重试——
// ICFQS*Topic*/QuotesBatch 走 address，ICFQSLHB*/MRFP* 走 hotAddress。
func NewICFQS(opts ...ICFQSOption) *ICFQSClient {
	client := &ICFQSClient{
		address:    DefaultICFQSAddress,
		hotAddress: DefaultICFQSHotAddress,
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client
}

// NewICFQSHot 创建一个面向 hot.icfqs.com 的 ICFQS 客户端。
func NewICFQSHot(opts ...ICFQSOption) *ICFQSClient {
	client := NewICFQS(WithICFQSAddress(DefaultICFQSHotAddress))
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client
}

// WithICFQSAddress 设置 ICFQS 的 host:port 或 base URL。
func WithICFQSAddress(address string) ICFQSOption {
	return func(client *ICFQSClient) {
		if strings.TrimSpace(address) != "" {
			client.address = strings.TrimSpace(address)
		}
	}
}

// WithICFQSHTTPClient 设置底层 HTTP 客户端。
func WithICFQSHTTPClient(httpClient *http.Client) ICFQSOption {
	return func(client *ICFQSClient) {
		if httpClient != nil {
			client.httpClient = httpClient
		}
	}
}

// WithICFQSTimeout 设置 HTTP 超时。
func WithICFQSTimeout(timeout time.Duration) ICFQSOption {
	return func(client *ICFQSClient) {
		if timeout > 0 {
			client.httpClient.Timeout = timeout
		}
	}
}

// WithICFQSHotAddress 设置龙虎榜/复盘类接口使用的 hot 域名地址。
func WithICFQSHotAddress(address string) ICFQSOption {
	return func(client *ICFQSClient) {
		if strings.TrimSpace(address) != "" {
			client.hotAddress = strings.TrimSpace(address)
		}
	}
}

// PostTQL 以 TQLEX 协议发送请求 (Params + oauth_zzfw)，解析响应内层 JSON 对象。
// 走普通均衡节点 (address)。entry 如 "CWServ.ph_tdxdatacenter_zttz_zy"。
func (client *ICFQSClient) PostTQL(ctx context.Context, entry string, params []any) (map[string]any, error) {
	body := map[string]any{
		"Params":     params,
		"oauth_zzfw": "1",
	}
	raw, err := client.post(ctx, entry, body, client.address)
	if err != nil {
		return nil, err
	}
	return parseICFQSTQLResponse(raw)
}

// PostTQLHot 同 PostTQL，但走 hot 域名 (hotAddress)。
// 龙虎榜/复盘类 (cfg_*) 接口仅在该域名开放。
func (client *ICFQSClient) PostTQLHot(ctx context.Context, entry string, params []any) (map[string]any, error) {
	body := map[string]any{
		"Params":     params,
		"oauth_zzfw": "1",
	}
	raw, err := client.post(ctx, entry, body, client.hotAddress)
	if err != nil {
		return nil, err
	}
	return parseICFQSTQLResponse(raw)
}

// PostJSON 以原始 JSON 体发送请求，直接解码标准 JSON 响应。走普通均衡节点。
func (client *ICFQSClient) PostJSON(ctx context.Context, entry string, body any) (map[string]any, error) {
	raw, err := client.post(ctx, entry, body, client.address)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ICFQSFormatTables 将 ResultSets 转换为表格视图，列名取自 ColName 或 ColDes.Name。
func ICFQSFormatTables(raw map[string]any) []ICFQSTable {
	resultSets, _ := raw["ResultSets"].([]any)
	tables := make([]ICFQSTable, 0, len(resultSets))
	for _, resultSet := range resultSets {
		rs, _ := resultSet.(map[string]any)
		columns := icfqsColumns(rs)
		content, _ := rs["Content"].([]any)
		table := ICFQSTable{Rows: make([]map[string]any, 0, len(content))}
		for _, rawRow := range content {
			values, _ := rawRow.([]any)
			row := make(map[string]any, len(columns))
			for i, column := range columns {
				if i < len(values) {
					row[column] = values[i]
				} else {
					row[column] = nil
				}
			}
			table.Rows = append(table.Rows, row)
		}
		tables = append(tables, table)
	}
	return tables
}

// ---------------------------------------------------------------------------
// 题材/概念板块 (涨停原因来源)
// ---------------------------------------------------------------------------

// ICFQSTopicListRaw 按类别查询题材列表。
// category 如 "gn" (概念), setcode 如 "1", page 从 1 开始。
func (client *ICFQSClient) ICFQSTopicListRaw(ctx context.Context, category string, setcode string, page int) (map[string]any, error) {
	if page <= 0 {
		page = 1
	}
	return client.PostTQL(ctx, "CWServ.ph_tdxdatacenter_zttz_zy", []any{"00601", category + "|" + setcode, page})
}

// ICFQSSearchTopicsRaw 按关键词搜索题材。
func (client *ICFQSClient) ICFQSSearchTopicsRaw(ctx context.Context, keyword string) (map[string]any, error) {
	return client.PostTQL(ctx, "CWServ.ph_tdxdatacenter_zttz_zy", []any{"00102", keyword, "0"})
}

// ICFQSNewTopicsRaw 查询新增题材 (新概念)。
func (client *ICFQSClient) ICFQSNewTopicsRaw(ctx context.Context) (map[string]any, error) {
	return client.PostTQL(ctx, "DataAggregation.zttz_xzgn2", []any{"01001", "", 1})
}

// ICFQSHotTopicsRaw 查询热门题材。
func (client *ICFQSClient) ICFQSHotTopicsRaw(ctx context.Context) (map[string]any, error) {
	return client.PostTQL(ctx, "CWServ.ph_tdxdatacenter_zttz_zy", []any{"00302", "", 1})
}

// ICFQSEventsRaw 查询题材事件驱动。
func (client *ICFQSClient) ICFQSEventsRaw(ctx context.Context) (map[string]any, error) {
	return client.PostTQL(ctx, "DataAggregation.zttz_jhqz", []any{"00401", "", 10})
}

// ICFQSTopTopicsRaw 查询领涨题材 (前 topN 个)。
func (client *ICFQSClient) ICFQSTopTopicsRaw(ctx context.Context, topN int) (map[string]any, error) {
	if topN <= 0 {
		topN = 10
	}
	return client.PostTQL(ctx, "CWServ.ph_tdxdatacenter_zttz_zy", []any{"00101", "", topN})
}

// ICFQSTopicDetailRaw 查询题材详情。
// code 为题材代码，setcode 为题材类别代码。
func (client *ICFQSClient) ICFQSTopicDetailRaw(ctx context.Context, code string, setcode string) (map[string]any, error) {
	return client.PostTQL(ctx, "CWServ.ph_tdxdatacenter_zttz_xqy", []any{"00301", code, setcode})
}

// ICFQSTopicKLineRaw 查询题材 K 线趋势数据。
func (client *ICFQSClient) ICFQSTopicKLineRaw(ctx context.Context, code string, setcode string) (map[string]any, error) {
	return client.PostTQL(ctx, "CWServ.ph_tdxdatacenter_zttz_xqy_v2_qsid", []any{"00501", code, 3, "", setcode})
}

// ICFQSTopicStocksRaw 查询题材成分股。
// page 从 1 开始，size 每页数量 (默认 20)。
func (client *ICFQSClient) ICFQSTopicStocksRaw(ctx context.Context, code string, setcode string, page int, size int) (map[string]any, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	return client.PostTQL(ctx, "CWServ.ph_tdxdatacenter_zttz_xggp", []any{"00901", code, setcode, 1, size, 0, page})
}

// ICFQSTopicQuotesRaw 查询题材成分股行情快照。
func (client *ICFQSClient) ICFQSTopicQuotesRaw(ctx context.Context, codes []ICFQSCode) (map[string]any, error) {
	codeValues, setcodeValues := splitICFQSCodes(codes)
	body := []map[string]any{{
		"ReqId":    "200800",
		"modname":  "module_misc.dll",
		"Code":     codeValues,
		"PageSize": fmt.Sprintf("%d", len(codeValues)),
		"Page":     "0",
		"Desc":     "0",
		"Setcode":  setcodeValues,
		"Sort":     "0",
	}}
	return client.PostJSON(ctx, "HQServ.hq_nlp", body)
}

// ICFQSTopicRotationRaw 查询题材轮动数据。
func (client *ICFQSClient) ICFQSTopicRotationRaw(ctx context.Context, dataNum int, dataType int, dataDate int, themeType string) (map[string]any, error) {
	if dataNum == 0 {
		dataNum = 1
	}
	if dataType == 0 {
		dataType = 1
	}
	if dataDate == 0 {
		dataDate = 2
	}
	if themeType == "" {
		themeType = "0"
	}
	body := []map[string]any{{
		"ReqId":     "200773",
		"modname":   "mod_copilot.dll",
		"dataDate":  fmt.Sprintf("%d", dataDate),
		"dataType":  fmt.Sprintf("%d", dataType),
		"dataNum":   fmt.Sprintf("%d", dataNum),
		"themeType": themeType,
	}}
	return client.PostJSON(ctx, "HQServ.hq_nlp_copilot", body)
}

// ---------------------------------------------------------------------------
// 行情批量
// ---------------------------------------------------------------------------

// ICFQSQuotesBatchRaw 批量查询股票行情快照。
// wantColumns 指定要返回的列 (如 "CLOSE","NOW")，为空时取 CLOSE/NOW。
func (client *ICFQSClient) ICFQSQuotesBatchRaw(ctx context.Context, codes []ICFQSCode, wantColumns []string) (map[string]any, error) {
	if len(wantColumns) == 0 {
		wantColumns = []string{"CLOSE", "NOW"}
	}
	codeValues, setcodeValues := splitICFQSCodes(codes)
	body := map[string]any{
		"Setcode": setcodeValues,
		"Head":    map[string]any{"Target": 0},
		"WantCol": wantColumns,
		"Code":    codeValues,
	}
	return client.PostJSON(ctx, "HQServ.PBCombHQ", body)
}

// ---------------------------------------------------------------------------
// 龙虎榜 / 营业部 / 活跃资金
// ---------------------------------------------------------------------------

// ICFQSLHBDetailRaw 查询个股龙虎榜明细。
// symbol 为股票代码，startDate/endDate 格式 "2006-01-02"。
// 走 hot.icfqs.com (该 entry 仅在 hot 域名开放)。
func (client *ICFQSClient) ICFQSLHBDetailRaw(ctx context.Context, symbol string, startDate string, endDate string) (map[string]any, error) {
	return client.PostTQLHot(ctx, "CWServ.cfg_fx_yzlhb", []any{"yybxq", startDate, endDate, symbol, "", 0, 2000})
}

// ICFQSYYBDetailRaw 按营业部统计查询龙虎榜明细。
// yybName 为营业部名称。走 hot.icfqs.com。
func (client *ICFQSClient) ICFQSYYBDetailRaw(ctx context.Context, yybName string, startDate string, endDate string) (map[string]any, error) {
	return client.PostTQLHot(ctx, "CWServ.cfg_fx_yzlhb", []any{"tjyyb", startDate, endDate, "", yybName, 0, 2000})
}

// ICFQSYZDetailRaw 查询活跃资金详情。走 hot.icfqs.com。
func (client *ICFQSClient) ICFQSYZDetailRaw(ctx context.Context, code string, startDate string, endDate string) (map[string]any, error) {
	return client.PostTQLHot(ctx, "CWServ.cfg_fx_yzlhb", []any{"yzxq", startDate, endDate, code, "", 0, 2000})
}

// ---------------------------------------------------------------------------
// 每日必看 / 复盘
// ---------------------------------------------------------------------------

// ICFQSMRFPRaw 按 reviewType 查询每日复盘数据。
// reviewType 如 "rzdm" (热涨低迷), date 格式 "20060102" 或 "2006-01-02"。
// 走 hot.icfqs.com。
func (client *ICFQSClient) ICFQSMRFPRaw(ctx context.Context, reviewType string, date string, limit int) (map[string]any, error) {
	if limit <= 0 {
		limit = 30
	}
	return client.PostTQLHot(ctx, "CWServ.cfg_tk_mrfp", []any{date, reviewType, "", 0, limit})
}

// ICFQSMRFPLatestDateRaw 查询每日复盘数据的最新可用日期。走 hot.icfqs.com。
func (client *ICFQSClient) ICFQSMRFPLatestDateRaw(ctx context.Context) (map[string]any, error) {
	return client.PostTQLHot(ctx, "CWServ.cfg_tk_mrfp", []any{"0", "rq", "", 0, 30})
}

// ---------------------------------------------------------------------------
// 内部实现
// ---------------------------------------------------------------------------

func (client *ICFQSClient) post(ctx context.Context, entry string, body any, address string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.tqlURLAt(entry, address), bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("icfqs %s returned status %d: %s", entry, resp.StatusCode, icfqsPreview(string(raw), 200))
	}
	return raw, nil
}

func (client *ICFQSClient) tqlURLAt(entry string, address string) string {
	address = strings.TrimRight(address, "/")
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "http://" + address
	}
	return address + "/TQLEX?Entry=" + entry
}

// parseICFQSTQLResponse 从响应文本中提取首个完整 JSON 对象并解析。
// TQLEX 响应常在 JSON 前后带有非 JSON 文本，需用括号深度匹配定位对象边界。
func parseICFQSTQLResponse(raw []byte) (map[string]any, error) {
	start := bytes.IndexByte(raw, '{')
	if start < 0 {
		return nil, fmt.Errorf("cannot parse icfqs response: %s", icfqsPreview(string(raw), 200))
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				var result map[string]any
				if err := json.Unmarshal(raw[start:i+1], &result); err != nil {
					return nil, err
				}
				return result, nil
			}
		}
	}
	return nil, fmt.Errorf("cannot parse icfqs response: %s", icfqsPreview(string(raw), 200))
}

func icfqsColumns(resultSet map[string]any) []string {
	if rawColumns, ok := resultSet["ColName"].([]any); ok && len(rawColumns) > 0 {
		columns := make([]string, 0, len(rawColumns))
		for _, rawColumn := range rawColumns {
			columns = append(columns, fmt.Sprint(rawColumn))
		}
		return columns
	}
	if rawColumnDefs, ok := resultSet["ColDes"].([]any); ok {
		columns := make([]string, 0, len(rawColumnDefs))
		for _, rawColumnDef := range rawColumnDefs {
			if columnDef, ok := rawColumnDef.(map[string]any); ok {
				columns = append(columns, fmt.Sprint(columnDef["Name"]))
			}
		}
		return columns
	}
	return nil
}

func splitICFQSCodes(codes []ICFQSCode) ([]string, []string) {
	codeValues := make([]string, 0, len(codes))
	setcodeValues := make([]string, 0, len(codes))
	for _, code := range codes {
		codeValues = append(codeValues, code.Code)
		setcodeValues = append(setcodeValues, code.Setcode)
	}
	return codeValues, setcodeValues
}

func icfqsPreview(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max]
}
