# 通达信协议解析

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

这个仓库是一个 Go 版通达信协议客户端，当前重点覆盖 7709 主行情协议、部分板块协议，以及 7727 扩展行情中的基金 K 线能力。

仓库同时提供两层能力：

- Go 库：适合在程序里直接调用协议接口。
- tdx-cli：适合调试协议、抓样本、导出 JSON 或验证新字段。

## 参考资源

- [gotdx](https://github.com/bensema/gotdx)
- [mootdx](https://github.com/mootdx/mootdx)
- [tdx2db](https://github.com/jing2uo/tdx2db)
- [niexq-tdx](https://github.com/niexqc/niexq-tdx)

## 当前能力

已实现的主要接口包括：

- K 线、分时图、分时成交、历史分时成交
- 五档盘口快照、批量快照
- 财务数据、除权除息、F10 信息
- 代码表、排序行情列表
- 板块成分股、涨跌停扫描
- 板块热榜和板块热力图数据集

其中板块相关能力目前可直接用于生成类似客户端“热门板块”“板块块图”的数据底座。

## 安装

```bash
go get github.com/millken/tdx
```

要求：

- Go 1.26+
- 可访问通达信 7709 / 7727 行情服务器

## Go 库快速开始

下面示例会自动选择可用主行情节点，并读取一笔实时盘口快照。

```go
package main

import (
	"fmt"

	"github.com/millken/tdx"
)

func main() {
	c, err := tdx.DialBest(nil)
	if err != nil {
		panic(err)
	}
	defer c.Close()

	tick, err := c.GetTick("sh600000")
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s price=%.2f prev=%.2f amount=%.0f\n", tick.Code, tick.Price, tick.PrevClose, tick.Amount)
}
```

常用接口：

- `GetKline(code, period, count)`
- `GetTick(code)`
- `GetBatchQuotes(codes)`
- `GetQuotesList(category, sortType, start, count, reverse)`
- `GetBoardMembers(board, sortType, count, sortOrder)`

## CLI

构建：

```bash
go build -o tdx-cli ./cmd/tdx-cli
```

查看命令帮助：

```bash
./tdx-cli
./tdx-cli probe
```

常用命令：

| 命令 | 说明 |
| --- | --- |
| `probe` | 探测主行情或 SP 节点 |
| `kline` | K 线数据 |
| `tick` | 单只股票实时快照 |
| `ticks` | 多只股票批量快照 |
| `tickchart` | 分时图 |
| `transaction` | 分时成交 |
| `history-trade` | 历史分时成交 |
| `codelist` | 代码表 |
| `finance` | 财务数据 |
| `xdxr` | 除权除息 |
| `company` / `company-content` | F10 信息 |
| `topboard` | 九宫格榜单 |
| `quotes` | 排序行情列表 |
| `bq` | 批量紧凑行情快照 |
| `board-members` | 板块成分股 |
| `limit` | 涨跌停扫描 |
| `hot-board` | 热门板块 + 板块内热门个股 |
| `board-heatmap` | 板块块图数据集 |
| `lhb` | 龙虎榜 |

## 常用示例

### 基础行情

```bash
./tdx-cli tick -code sz000001
./tdx-cli kline -code sh600000 -period day -count 20
./tdx-cli ticks -codes sh600000,sz000001,sz300750
./tdx-cli bq -codes sh600000,sz000001,sz300750
```

### 排序行情

```bash
./tdx-cli quotes -market 6 -sort change_pct -count 20
./tdx-cli -format json quotes -market 10001 -sort short_turnover -count 10
./tdx-cli -format json quotes -market 6 -sort amount_2m -count 20
```

### 板块命令

```bash
./tdx-cli board-members -board 881314 -count 20
./tdx-cli hot-board -category gn -board-count 10 -stock-count 3
./tdx-cli -format json board-heatmap -category hy -count 40 -board 881314 -member-count 20
./tdx-cli limit -board 6 -type up
```

### F10 / 财务

```bash
./tdx-cli finance -code sh600000
./tdx-cli company -code sh600000
./tdx-cli company-content -code sh600000 -id 1
```

## 板块能力说明

### `hot-board`

用于获取一组服务器排序后的热门板块，并为每个板块补一组热门成分股。

适合：

- 终端调试“当前热门板块”
- 做板块榜单接口
- 做板块 -> 个股联动列表

输出特征：

- 保留服务器原始排序，不会再用本地热度分重新排序
- 额外补充板块名称
- 额外补充展示用 `heat` 分值

### `board-heatmap`

用于生成“板块块图 / 热力图”所需的结构化数据。

输出包括：

- `selected_board`：当前选中的板块快照
- `boards`：左侧板块列表
- `members`：选中板块的成分股列表

板块快照里目前已补齐这些高频字段：

- `price`
- `pre_close`
- `change_pct`
- `change_3d`（由板块日线最近 4 根 bar 计算）
- `amount`
- `volume`
- `cur_volume`
- `server_time`
- `rise_speed`
- `short_turnover`
- `amount_2m`
- `vol_ratio`
- `depth`

另外：

- `members` 里的成分股现在已包含 `main_net`
- `selected_board` 现在会额外给出聚合后的 `main_net`

## 协议与连接说明

板块命令涉及两类连接：

- 主行情连接：用于 `quotes`、`tick`、`bq` 等 7709 主行情协议
- 板块连接：用于 `board-members`、`limit` 等板块成分股协议

当前仓库里，板块命令的稳定连接方式是：

- 地址来源使用 `BestSPAddresses(0)`
- 连接方式仍使用普通 `Dial`，而不是 `DialSP`

如果你自己扩展板块相关命令，这个细节需要保留，否则容易卡在 SP 登录阶段。

## `quotes` / `bq` 字段说明

`quotes` 和 `bq` 已经可以直接拿到这些更实用的盘中字段：

- `server_time`
- `rise_speed`
- `short_turnover`
- `amount_2m`
- `vol_ratio`
- `depth`

另外，`quotes` 还会返回：

- `in_vol`
- `out_vol`
- `cur_vol`

## 已知限制

### 1. `quotes` 的部分排序字段目前只能排序，不能取值

以下排序在服务端可用：

- `strength`
- `vol_speed`
- `main_net`

但当前实测的 `0x054B` 响应里，没有发现这三个值的实际返回字段。现阶段结论是：

- 可以用它们做服务端排序
- 不能从当前响应体里直接取到对应数值

补充说明：

- `board-members` 的 `0x122C` 已实测可解出成分股 `main_net`
- `board-heatmap` 的 `selected_board.main_net` 当前来自成分股 `main_net` 汇总，不是 `0x054B` 直接回包字段

### 2. `server_time` 存在混合编码

通达信返回的 `server_time` 在不同标的上存在两种编码方式。仓库里已经做了兼容解码，但如果你在逆向新协议时看到原始值异常，不要默认它一定是标准 `HHMMSSmmm`。

### 3. 板块热度分是展示字段

`hot-board` 和成员股里的 `heat` 仅用于展示，不应用来覆盖服务器原始排序。

## 调试建议

如果你在继续逆向协议字段，推荐直接使用 CLI：

```bash
./tdx-cli -format json quotes -market 6 -sort change_pct -count 20
./tdx-cli -format json quotes -market 10001 -sort short_turnover -count 20
./tdx-cli -format json board-heatmap -category hy -count 40 -board 881314 -member-count 20
```

仓库里也保留了一些验证命令：

- `cmd/verify_fund_detail`
- `cmd/verify_fund_kline`

以及仅在显式开启时执行的 live probe 测试，用于协议字段反推。

## 免责声明

1. 本项目仅供学习、研究和技术交流使用，禁止用于商业或非法用途。
2. 使用本项目产生的任何数据、损失或法律责任，作者不承担任何责任。
3. 对第三方服务器或服务的访问，使用者需自行遵守相关法律法规及服务协议。

## 许可证

MIT，详见 [LICENSE](LICENSE)。

