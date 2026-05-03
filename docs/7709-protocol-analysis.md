# 7709 行情协议完整分析

## 一、通信配置

### TAEngine 配置 (XML)
```xml
<TAEngine>
  <CoreTimeout JobTimeout="150000"/>
  <CoreMemory ReqBufSize="8192" AnsBufSize="65535"/>
  <Packet ReqSegmentSize="8192" AckSegmentSize="-1"/>
  <Timeout Create="10000" Transaction="10000"/>
  <HeartBeat TimeSpan="30" InetDebug="NO" OnIdle="YES"/>
  <Compress Mode="0" MinSize="1024"/>
  <Channel CheckConnect="3"/>
</TAEngine>
```

### 配置参数说明
| 参数 | 值 | 说明 |
|------|-----|------|
| JobTimeout | 150000ms | 任务超时时间 |
| ReqBufSize | 8192 | 请求缓冲区大小 |
| AnsBufSize | 65535 | 应答缓冲区大小 |
| ReqSegmentSize | 8192 | 请求分片大小 |
| HeartBeat TimeSpan | 30s | 心跳间隔 |
| CheckConnect | 3 | 连接检查间隔 |

## 二、消息类型

### 2.1 行情推送消息

#### Recv 111 - 行情快照推送
```
Recv 111 PushData code=<股票代码> ItemNum=<数据项数量>
├── Now=<现价>
├── Open=<开盘价>
├── Close=<昨收价>
├── Vol=<成交量>
├── sell=<卖价>, vol=<卖量>
└── buy=<买价>, vol=<买量>
```

#### Recv 112 - 买卖盘队列推送
```
Recv 112 PushData
├── buy1num=<买盘档位数>
└── sell1num=<卖盘档位数>
```

#### Recv 112 SetQueue - 盘口数据
```
Recv 112 SetQueue buy1num=<数量> sell1num=<数量>
```

### 2.2 请求/应答消息类型

| 消息类型 | 请求结构 | 应答结构 | 用途 |
|---------|---------|---------|------|
| Tick逐笔 | mp_tick_req | mp_tick_ans | 逐笔成交数据 |
| 暂存数据 | mp_zst_req | mp_zst_ans | 盘口/队列数据 |
| 分时数据 | mp_fxt_req | mp_fxt_ans | 分时图数据 |
| 行情信息 | mp_hqinfo_req | mp_hqinfo_ans | 基础行情 |
| 组合行情 | mp_combhq_req | mp_mask_ans | 多股行情 |
| F10配置 | mp_f10cfg_req | - | F10资料配置 |
| F10文本 | mp_f10txt_req | - | F10资讯文本 |
| 资讯标题 | mp_infotitle_req | - | 信息地雷标题 |

## 三、数据包格式

### 3.1 包头格式
```
+----------------+----------------+
| HeaderSize     | DataLength     |
+----------------+----------------+
| ReceiveBufLen  | ...            |
+----------------+----------------+
```

### 3.2 分片传输
- 支持大数据包分片传输
- Recv Fragment, Data Size
- 最大分片大小: 8192 字节

## 四、股票订阅格式

### 4.1 订阅请求
```json
{
  "CodeList": [
    {"SetCode": "1", "Code": "600000"},
    {"SetCode": "2", "Code": "000001"}
  ],
  "GroupName": "自选股组名"
}
```

### 4.2 SetCode 市场代码
| SetCode | 市场 |
|---------|------|
| 1 | 上海A股 |
| 2 | 深圳A股 |
| 其他 | 对应不同市场 |

## 五、登录认证流程

```
客户端 → 服务器: Connect
服务器 → 客户端: 200 connection established
客户端 → 服务器: Login (client_login_info)
服务器 → 客户端: CTDXSession OnSessionLoginSuccess
```

### 状态枚举
- State_Logining - 登录中
- ConnectOk - 连接成功
- DisConnect - 断开连接

## 六、心跳保活

```
客户端 → 服务器: send_alive (支持 XGuard)
服务器 → 客户端: recv_alive
```

参数:
- PolicyMuted: 策略静默
- SupportXGuard: 支持 XGuard
- XGuardReady: XGuard 就绪
- cbCipher: 加密回调

## 七、数据维护模式

| 参数 | 说明 |
|------|------|
| bHQInfoSet | 行情信息设置 |
| bTickSet | Tick数据设置 |
| nHQMaintainMode | 行情维护模式 |
| GapTime | 数据间隔时间 |

## 八、时间同步

```
StartTimeServer=<服务器时间>,TimeLocal=<本地时间>,OffSet=<偏移>
HQTime=<行情时间>,HQDate=<行情日期>,Gap=<时差>
```

## 九、版本管理

```
LocalVer=<本地版本>,ServerVer=<服务器版本>
LocalChange=<本地变更>,FroceUp=<强制升级>,MergeDown=<合并下载>
```

## 十、服务器列表 (端口 7709)

| 服务器 | IP地址 |
|--------|--------|
| 广州电信双线站点1 | 110.41.147.114 |
| 广州电信双线站点2 | 110.41.2.72 |
| 上海电信双线站点1 | 124.70.176.52 |
| 北京联通双线站点1 | 121.36.54.217 |
| ... | ... |

(共38个行情服务器)

## 十一、逆向补充

- 当前可以把“旧协议同步 / 新协议异步”的边界单独写清：
  - 仓库 legacy 客户端在 [client.go](client.go#L198) 的 `SendFrame` 里采用的是同步调用模型：发送一帧后立即按 `msgID` 阻塞等待单次响应返回
  - 这和旧接口 `GetCount/GetCode/GetGbbq/...` 的调用方式一致，本质上是 request/response 型同步协议
  - 但 Linux 新客户端主链已经不是这一模型：`libTdxAsioComm.so` 明确建立在 `boost::asio` 上，`libViewthem.so` 的正文发送又落在 `0x42040 / 0x42060 -> 0x42f00 -> 0x42270` 的排队发送 + worker 路径，而不是同步 `send/read` 包装器
  - 已确认的同步 `send/read` 包装器 `0x428f0 / 0x42970 / 0x429f0` 目前只覆盖代理 CONNECT 和前置握手，不是 7709 长会话正文
  - 再结合本文件前文已有的 `Recv 111/112 PushData` 推送特征，以及 `stream 42` 的长连接分期，当前更稳的理解是：新协议主模型是“长连接 + 异步状态机 + 队列化发送 + 服务端推送”，旧协议主模型才是“同步请求/同步应答”

- `libViewthem.so:0xa4ae0` 当前已经可以看成资讯类请求的通用 payload builder。默认分支会组一个固定 `0x1e` 字节内部 payload，tag 为 `0x1f44`，随后调用 `0xada60` 下送到统一 producer。
- 这个 helper 本身不决定外层命令对。当前已确认至少三类 caller 会先写全局 `0x4ecf58/0x4ecf5a`，再复用 `0xa4ae0`：
  - `0x113110/0x1132b0 -> 0x00/0x6f`
  - `0xe0d20/0xe0ee0 -> 0x00/0x88`
  - `0x45ad8/0x45d8b -> dynamic/0x30`
- `0xfd0` INFO dialog 的两条高价值路径已经闭合：
  - `0x113110` 直接从 `obj+0xd0` 这条内嵌 `0x9a` 记录抽字段，再走 `0x00/0x6f -> 0xa4ae0`
  - `0x1132b0` 先把外部 `0x9a` 记录整块复制到 `obj+0xd0..`，随后也立即走 `0x00/0x6f -> 0xa4ae0`
- 这说明当前更接近真实请求参数来源的，不是 `0x4fbdc0` 头快照，而是 INFO 标题/条目对象化后的 `0x9a` 记录本体。
- `0xa4ae0` 还有一条由全局 `0x572558` 控制的替代分支：命中时会改组 `0x48` 字节、tag 为 `0x264b` 或 `0x264e` 的记录，再同样经 `0xada60` 下送。当前更像资讯请求模式切换，而不是另一套独立传输层。
- `panzhong.pcapng` 的长会话 `tcp.stream==42` 已确认与 `tdxrpc_new/protocol/1728_stream.txt` 中的 `c502/b906` 样本同族，不是另一套全新正文格式。两者共同结构都是：前 6 字节 session wrapper、随后两个重复的 little-endian 长度字段、再跟 little-endian 命令字与 body。
- 在 `stream 42` 里，这 6 字节 wrapper 被统一清成 `00 00 00 00 00 00`；而在 `1728_stream.txt` 里，相同位置仍是 `0c xx 18 xx seq` 这一组阶段/服务/序号字段。也就是说，长会话变化的是会话层封装，不是 `c502/b906` 的业务 body。
- 当前已坐实两类资源同步命令：
  - `c502`：40 字节文件名请求，例如 `infoharbor_block.dat`、`tdxhhy.cfg`、`infoharbor_ex.name`、`infoharbor_ex.code`
  - `b906`：分块资源拉取，请求体形如 `offset32 + 0x00007530 + 固定长度文件名区`，服务端回 `b1cb7400...` 后跟 zlib 压缩数据
- 这意味着当前主会话前半段更偏“资源/配置同步”，并不直接对应此前重点追踪的 `0x1f47 / 0x1f4c / 0x1f80 / 0x1f44` 业务 payload 家族。后续对齐 builder 时，应把 `c502/b906` 单独视为文件/资源获取子链。
- 继续向后拆 `stream 42` 后，又能看到资源同步之后的第二阶段：客户端不再发送 `c502/b906`，而是开始发送 `0x0010` 与 `0x000f` 两类批量代码列表请求。外层仍是 `6-byte wrapper + lenLE + lenLE + cmdLE + body`，但 body 已经变成“批次数 + NUL 分隔 6 位代码串”。
- `0x0010` 阶段样本：
  - `frame 921`：`count=100`，代码从 `000050/000551/.../159228` 开始
  - `frame 928`：`count=100`，继续 `159229..159378`
  - `frame 1002`：`count=38`，构成这一阶段的短尾包
- `0x0010` 的响应会直接回同命令字的大块压缩体，例如 `frame 924` 的响应头可读成 `cmd=0x0010, zipLen=0x0f6b, rawLen=0x37de`。
- `0x000f` 阶段样本：
  - `frame 1029`：`count=87`
  - `frame 1032`：`count=67`
  - `frame 1035`：`count=30`
  - 代码内容明显切到另一批更像行情股票池的集合，例如 `300190/300243/300277/.../920961`
- `0x000f` 的响应体更大，例如 `frame 1030` 的响应头可读成 `cmd=0x000f, zipLen=0x2144, rawLen=0x665c`。
- 现有仓库的 legacy 常量还能提供一个保守语义锚点：`0x0010` 在旧协议里长期对应 `FINANCE/KMSG_FINANCEINFO`，`0x000f` 对应 `EXDIVIDEND/KMSG_XDXRINFO`。因此当前长会话这两段很可能与“财务信息 / 除权除息信息”相关。
- 但它们已经不是 legacy 单股请求的编码形式。旧的单股 `0x000f` 请求只是短小的 `01 00 + exchange + 6位code`；`stream 42` 里观察到的则是 `count32 + NUL 分隔代码串` 的批量容器。更稳的说法应是：这里出现的是“财务/复权相关数据的批量预取阶段”，而不是把旧 frame 原样搬进长会话。
- 因而当前更稳的会话分层是：
  - 前半段：`c502/b906` 资源同步
  - 中段：`0x0010` 批量代码列表请求
  - 后续：`0x000f` 批量代码列表请求
- `0x000f` 之后没有继续切出新的正文命令族。`frame 1041` 起客户端会短暂回切到一组资源尾包：先 `c502("zd.zip")`，再 `b906("zd.zip", offset=0)` 与 `b906("zd.zip", offset=0x7530)`。
- 最后一条 `b906` 响应不是新命令，而是同一压缩体被拆成多个 TCP 段下发：`frame 1053`、`frame 1055`、`frame 1057`，随后 `1059/1060` 双向 FIN 收尾。
- 因而当前最稳的结论是：主长会话的关键应用层分期仍以“资源同步 -> 0x0010 -> 0x000f”为主，尾部只是回到一次小型 `zd.zip` 资源收尾。
- 这说明主长会话在资源同步结束后，确实切进了更接近行情取数的正文阶段；但这条正文子链目前仍未和 `0x1f47 / 0x1f4c / 0x1f80 / 0x1f44` builder 家族直接对上，暂时应分开建模。
