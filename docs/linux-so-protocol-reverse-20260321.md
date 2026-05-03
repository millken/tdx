# Linux 版 SO 协议层逆向记录

## 目标

- 定位 Linux 版客户端中新协议数据包的生成位置。
- 区分业务组包层、通信对象层、纯网络传输层。
- 继续向首个 connect 包的字段级结构推进。

## 当前结论

### 1. 分层定位

- `libViewthem.so`：业务组包层，负责构造发送缓冲区，并直接调用发包接口。
- `libTdxAsioComm.so`：通信对象层，提供 `MakeUserCommModule` / `DelUserCommModule`，内部是 `boost::asio` 的 TCP 异步通信框架。
- `libTGear.so`：纯传输层，提供 `DirectConnect` / `SendToSocket` / `ReceiveBytes` 这类 socket 薄封装。

### 2. 不是谁在组协议包

- `libTGear.so` 中的 `SendToSocket` 只是循环调用 `WS_send`。
- `libTGear.so` 中的 `DirectConnect` 只是创建 socket 并连接远端。
- 因此新协议包不是在 `libTGear.so` 中生成的。

### 3. 新协议包的直接生成位置

- `libViewthem.so` 直接导入：
  - `ReceiveBytes`
  - `SendToSocket`
  - `DirectConnect`
  - `MakeUserCommModule`
- 在 `libViewthem.so` 中已定位多处发送点：
  - `0x11cfe2`
  - `0x11d2ef`
  - `0x121e5f`
- 这些发送点都遵循同一模式：
  - 先在栈/堆上分配并清零缓冲区
  - 把多个片段 `memcpy`/`strncpy` 到发送缓冲区
  - 调 `SendToSocket`
  - 立刻调用 `ReceiveBytes` 读取 16 字节头
  - 再按响应头长度继续读取包体
  - 必要时调用 `uncompress`

## 证据链

### 1. `libTdxAsioComm.so` 是通信对象层

- 导出：
  - `MakeUserCommModule`
  - `DelUserCommModule`
- 反混淆动态符号中存在大量 `boost::asio` / `CUserComm` 相关符号。
- 说明其核心职责是封装连接对象和异步通信，不直接承载业务包格式。

### 2. `libTGear.so` 是网络薄封装层

- 导出：
  - `DirectConnect`
  - `SendToSocket`
- `SendToSocket` 反汇编显示：
  - 调 `WS_send`
  - 处理 `0x2733` 类非阻塞错误
  - 循环直到发送完成或失败
- `ReceiveBytes` 反汇编显示：
  - 调 `WS_recv`
  - 循环接收到指定长度
- `DirectConnect` 反汇编显示：
  - `WS_socket`
  - 后续连接逻辑
- 以上都没有业务协议字段拼装逻辑。

### 3. `libViewthem.so` 是业务组包层

- 直接依赖：
  - `libtdxdllbase.so`
  - `libTGear.so`
  - `libTdxAsioComm.so`
- 在 `libViewthem.so` 中发现：
  - `INFO_Connect`
  - `INFO_IsConnect`
  - `INFO_DisConnect`
- 说明它对外暴露了信息/连接入口，同时内部直接触发网络通信。

## 连接建立路径

### 1. `INFO_Connect`

- 导出入口：`0x128be0`
- 该函数负责创建连接相关 UI/对象，不直接组协议包。

### 2. 主机连接尝试

- `libViewthem.so` 中 `0x122170` 附近的函数在成功返回前会执行三次 `DirectConnect`：
  - `0x122218`
  - `0x1222a5`
  - `0x1222ee`
- 该函数会从一块模板/配置区复制 `0xa9` 字节到临时区，再分别取偏移：
  - `+0x01`
  - `+0x33`
  - `+0x65`
- 高概率对应 `connect.cfg` 中的多个候选主机条目。

### 3. 连接成功后的直接下一跳

- 在 `0x12222b`，连接成功后立即调用 `0x121d50`。
- 这说明 `0x121d50` 才是“连接成功后的首个协议交互函数”。
- 另一处早期发送函数 `0x11cf00` 的调用来源是 `0x118453`，不是 connect 成功后的直接下游，因此不是首个 connect 包的优先候选。

## 第一个 connect 包的当前定位

### 1. 首个 connect 包对应函数

- 当前最优候选：`libViewthem.so` 中从 `0x121d50` 开始的函数。
- 关键发送点：`0x121e5f`

### 2. 组包过程

- 先申请 `0x6000` 字节堆缓冲区。
- 栈上从 `0x30(%rsp)` 开始准备一个 `0x47` 字节的临时块。
- 关键字段写入：
  - `0x121dbd`：写入 `0x2726` 到 `0x30(%rsp)`
  - `0x121ded`：把对象 `r12 + 0xc0` 处的值写入 `0x32(%rsp)`
  - `0x121e0c`：`GetNetCardStr` 写入 `0x36(%rsp)` 一带
  - `0x121e15`：取对象 `r12 + 0x1110` 处 1 字节值
  - `0x121e32`：该字节写入 `0x76(%rsp)`
- 随后把发送缓冲区分两段拷入：
  - 前 `0x0a` 字节来自 `0x16(%rsp)`
  - 后 `0x47` 字节来自 `0x30(%rsp)`
- 最终发送总长度：`0x51`

### 3. 当前可恢复的首包布局

- 总长度：`0x51`
- 可恢复的布局如下：

```text
offset  size  value/source
0x00    0x06  00 00 00 00 00 00
0x06    0x02  47 00
0x08    0x02  47 00
0x0A    0x02  26 27
0x0C    0x04  *(r12 + 0xC0)
0x10    0x40  GetNetCardStr(...) 写入的网卡字符串区
0x50    0x01  *(r12 + 0x1110)
```

- 其中前 10 字节可以直接恢复为：

```text
00 00 00 00 00 00 47 00 47 00
```

- 47 字节负载的开头可恢复为：

```text
26 27 ?? ?? ?? ?? [netcard...] [flag]
```

- 这说明首个 connect 包至少由“固定前缀 + 命令/类型 + 对象状态 + 网卡信息 + 单字节标志”组成。

### 4. 响应读取模式

- `SendToSocket(..., 0x51)` 后：
  - 先 `ReceiveBytes(..., 0x10)` 读取 16 字节头
  - 从响应头中取长度字段
  - 再继续 `ReceiveBytes` 读取响应体
- 如果响应头含压缩标志，则进入解压路径。

### 5. 当前推断

- 第一个 connect 包不是静态常量整体 `memcpy`，而是运行时按字段动态生成。
- 包中至少包含：
  - 一个 2 字节类型/命令字段（当前看到 `0x2726`）
  - 一个对象状态值（来自 `r12 + 0xc0`）
  - 网卡字符串（`GetNetCardStr`）
  - 一个 1 字节状态或标志位（来自 `r12 + 0x1110`）
- 由于未直接命中 `0c021894...` 这类完整模板，说明 Linux 版首包极可能不是用硬编码完整字节串下发，而是运行时拼接。

## 其他发送点

### 1. `0x11cf00` 函数内发送点 `0x11cfe2`

- 发送长度：`0x34`
- 由两段组成：
  - 前 `0x0a` 字节
  - 后 `0x2a` 字节
- 发送后同样是 `ReceiveBytes(0x10)` + 按长度续收。
- 当前已确认它是另一路业务发送函数，但不是 connect 成功后的第一跳。

### 2. `0x11d2ef`

- 发送长度：`0x78`
- 后续存在解压和文件写出逻辑
- 更像后续阶段业务请求，不是首个 connect 包。

## 未决问题

- `0x2726` 在首个 connect 包中的语义尚未完全确认。
- `r12 + 0xc0` 与 `r12 + 0x1110` 两个字段的业务含义尚未命名。
- 尚未把 `0x121d50` 的首包与抓包中的某一条具体报文做一一对应。

## 下一步

- 继续从 `0x121d50` 向前后追，恢复首个 connect 包的完整字段布局。
- 动态 hook `SendToSocket` 时优先关注：
  - `0x121e5f`
  - `0x11cfe2`
  - `0x11d2ef`
- 用运行时十六进制发送缓冲区去对照现有抓包，确认首包对应关系。

## 2026-03-21 补充：方向纠偏与新主链

### 1. `libViewthem.so` 当前更像另一条旧事务链

- 目前在 `libViewthem.so` 中已定位到的 4 个 `SendToSocket` 发送点，其包长分别为：
  - `0x34`
  - `0x78`
  - `0x19e`
  - `0x51`
- 这 4 个事务都共享同一类外层格式：
  - 前 6 字节清零
  - 后跟两份重复长度字段
  - 发送后固定先收 16 字节响应头，再按长度续收
  - 条件性 `uncompress`
- 该格式与抓包中连接 `xxx:7709` 后的短阶段包序列不一致：
  - `0c 02 18 94 ...`
  - `0x1899`
  - `0x1869`
- 因此当前更合理的判断是：
  - `libViewthem.so` 这组事务不是目标抓包里的 `7709` 新行情主链
  - 更像旧事务链、资源链或另一条登录/初始化链

### 2. 页面层走的是 `CallTQL("HQServ.*")` 高层接口

- 在 `tc/files/lib/connect/req.js` 中，页面并不直接拼二进制包，而是调用：
  - `CallTQL(callback, "HQServ.HQInfo", json)`
  - `CallTQL(callback, "HQServ.ZST", json)`
  - `CallTQL(callback, "HQServ.FXT", json)`
  - `CallTQL(callback, "HQServ.Tick", json)`
  - `CallTQL(callback, "HQServ.MultiHQ", json)`
- 在 PC 模式下，最终出口是：
  - `window.external.CallTQL(callback_name, funcname, params)`
- 这说明页面层只负责：
  - 组织 JSON 参数
  - 指定高层功能名
  - 把请求交给本地桥接层
- 页面层本身不负责构造 `0x1894 / 0x1899 / 0x1869` 这类二进制行情帧。

### 3. 新的高优先级主链：`tcefwnd.so -> tdxframe100.so -> libtpdata.so`

#### `tcefwnd.so`

- 可见桥接相关痕迹：
  - `TCefWnd_RegisterCallBack`
  - `CallTQL`
  - `cefQuery`
  - `TQLEX`
- 其字符串中还包含注入式 JS 逻辑，说明它负责 CEF 页面和本地 native 之间的桥接。

#### `tdxframe100.so`

- 包含大量市场会话初始化入口：
  - `CTdxABztWnd::InitGPSession`

## 2026-03-21 补充：Ghidra Headless 自动化验证

### 1. headless 工作流已可直接复用

- 仓库内已落地：
  - `tools/ghidra/run_headless.sh`
  - `tools/ghidra/scripts/DumpVtable.java`
  - `tools/ghidra/scripts/ListMemoryBlocks.java`
  - `tools/ghidra/scripts/ApplySymbolMap.java`
- `run_headless.sh` 默认会隔离 `HOME/XDG_CONFIG_HOME`，避免本机损坏扩展干扰分析。

### 2. 当前 `libtaapiw.so` 的地址换算规则已校准

- Ghidra 对该 ELF 当前采用了 `0x100000` image base。
- 因此 objdump 地址要换算为：
  - `ghidra_addr = objdump_addr + 0x100000`
- 例如：
  - objdump `0x7879d0`
  - ghidra `0x8879d0`

### 3. 批量命名已经验证生效

- 首轮用 `ApplySymbolMap.java + libtaapiw-known-symbols.txt` 处理 `libtaapiw.so` 时，最初只创建了 label。
- 原因不是地址错，而是目标程序以 `--noanalysis` 导入，Ghidra 中这些入口还没有 function 对象。
- 现已修正脚本：
  - 对可执行地址先尝试反汇编
  - 再创建 function
  - 最后写入用户自定义符号名
- 修正后再次验证结果：
  - `renamedFunctions=31`
  - `createdLabels=1`

### 4. 这对后续逆向的直接价值

- 现在可以先批量把 `CTDXSession`、`CTDXSessionIface` 一批已知入口写回 Ghidra 项目。
- 后续再导 vtable、追 `+0x60/+0x68` 时，不需要反复手工对 objdump 偏移。
- 现已新增两个更适合未分析程序的 headless 辅助脚本：
  - `DumpFunctionRefs.java`：按函数地址导出完整指令、显式调用点、跳转点和显式数据引用
  - `DumpDataAt.java`：按地址直接读取 rodata；若 Ghidra 尚未建 data，也会回退读取原始 C 字符串
- 下一步应继续：
  - 扩充 `libtaapiw.so` 已知符号表
  - 再把同一套流程推广到 `libtpdata.so`、`libTdxAsioComm.so`、`libTGear.so`

### 5. `+0x60/+0x68` 对象链的最新 headless 结论

- `0x17bd10` 已可保守命名为 `CTDXSession_EnsureActiveExternalObject`：
  - 先检查 `this+0x68`
  - 若为空，则取 `this+0x60` 虚表 `+0x50`
  - 创建成功后回写到 `this+0x68`
- 创建成功后，该对象会经对象虚表 `+0x8` 吃一组字段和字符串常量：
  - `IdentityInfo`
  - `Separator`
- `0x178430` 已可保守命名为 `CTDXSession_BuildActionFromActiveContext`：
  - 先从 `this+0x3f0` 控制对象经虚表 `+0x78` 取上下文对象
  - 若还是默认实现 `0x181170`，则直接回退到控制对象 `+0x18`
  - 再把这个上下文对象与 `this+0x68` 一起交给 `this+0x60` 虚表 `+0x58`
  - 返回的新对象随后还会被写入 `JobType`
- `0x181170` 当前可保守命名为 `CTDXControl_DefaultGetContextObject`，它只是默认返回控制对象 `+0x18`。

### 6. `libtpdata.so` 的 headless 校准与首轮主链验证

- `libtpdata.so` 也已通过 Ghidra headless 导入，并确认当前 image base 同样是 `0x100000`。
- 因此此前 objdump 侧已知入口在 Ghidra 中可直接换算为：
  - `0x4c20c -> 0x14c20c`
  - `0x4ccb4 -> 0x14ccb4`
  - `0x4d064 -> 0x14d064`
  - `0x4d1a0 -> 0x14d1a0`

### 7. `libtpdata.so` 当前已由 headless 重新坐实的路径

- `0x4c20c` 已可保守命名为 `TPData_JobTypeDispatch`：
  - 先经上游虚表 `+0xb0` 填充本地 `0x18` 字节描述块
  - 再读取 `JobType`
  - 明确按 `2/5/3/7` 分派到：
    - `0x4c830`
    - `0x4ccb4`
    - `0x4cef8`
    - `0x4d064`
- `0x4ccb4` 已可保守命名为 `TPData_JobType5_TextToInetTQL`：
  - 先读取 `JobData`
  - 条件性读取 `ErrType`、`ErrInfo`、`ObjClsName`
  - 其中 `ObjClsName == CTAJob_InetTQL` 是关键分支条件之一
- `0x4d064` 已可保守命名为 `TPData_JobType7_ManagerSubmit`：
  - 会读取 `ErrInfo`、`ObjClsName`、`ErrType`
  - 在 `ErrType==0 && outerType==9` 时，经 `TPData_MsgWnd.+0x500` 与上层执行器继续下沉
- `0x4d1a0` 已可保守命名为 `TPData_InetTQLAssembler`：
  - 通过容器虚表 `+0x70` 创建类型为 `5`、名称为 `CTAJob_InetTQL` 的 job
  - 写入 `Name`、`Body`
  - 若外部 body 为空，则补默认 `Body="0"`
  - 随后经容器虚表 `+0x60` 把该 job 挂回 manager 容器

### 8. 本轮直接拿到的关键字符串

- `JobType`
- `JobData`
- `ErrType`
- `ErrInfo`
- `ObjClsName`
- `Name`
- `Body`
- `CTAJob_InetTQL`
- 默认 body 值：`0`

### 9. `libtpdata -> CAppCore` 这一跳的最新 headless 结论

- 由 `TPData_JobType7_ManagerSubmit` 可见：它会通过 `TPData_MsgWnd.+0x500` 调上层执行器虚表 `+0x100`。
- 进一步导出 `CAppCore` vtable 后，已确认该槽位对应 `libtaapiw.so:0x134110`。
- `0x134110` 的实际行为不是直接执行 job，而是：
  - 遍历内部对象数组
  - 对每个对象取“名字”
  - 与传入字符串比较
  - 命中后把该对象从数组中摘除并释放
- 因此当前更合理的保守命名是：`CAppCore_RemoveObjectByName`。
- 这说明 `type 7` 当前已确认的这一条下沉分支，更像“按名字同步/清理上层对象状态”，还不是最终 7709 发包点本体。

### 10. `libtpdata` pending map helper 的最新 headless 结论

- `0x4d40e` 当前可保守命名为 `TPData_TakePendingMapValue`：
  - 进入时会先锁 `+0x508`
  - 在 `+0x538` 这组 map/树结构里按 key 查找
  - 若找到，则把 value 写到输出地址并将该条目从容器中移除
- `0x4dac8` 当前可保守命名为 `TPData_LookupPendingMapSlot`：
  - 会在同一组 `+0x538` map/树结构里查找 key
  - 若需要则构造/定位条目
  - 返回 value 槽位地址，供上游写入待处理对象指针

### 11. `TPData_InetTQLAssembler` 直接入口的最新 headless 结论

- `0x4d1a0` 本体当前没有来自其他业务函数的大量直接 call；它的直接代码入口是紧邻其后的 `0x4d406`。
- `0x4d406` 只有两条有效指令：
  - `call 0x4d1a0`
  - `leave; ret`
- 因此当前可把 `0x4d406` 保守命名为 `TPData_InetTQLAssemblerCStringThunk`。
- 这说明上层很多路径真正落到的不是 `0x4d1a0` 本体入口，而是这个薄 thunk，再由 thunk 统一进入 assembler。

### 12. `TPData_MsgWnd` 上 `+0x500/+0x98/+0xd8` 的最新分工

- `0x4b66a` 的行为现在已经足够稳定，可保守命名为 `TPData_MsgWnd_EnsureAppCoreAndLazyService`：
  - 若 `+0x500` 为空，则通过 `TaApi_CreateAppCore` 创建 `CAppCore`
  - 随后对 `CAppCore` 调 vtable `+0x20`，传入 `this+0xe0`
  - 若 `+0x98` 为空，则再通过 `CAppCore` vtable `+0x158` 懒取附属 service 对象
- 结合 `0x4d1a0` 起始段当前可把三个字段关系收敛为：
  - `+0x500 = CAppCore*`
  - `+0x98 = 从 CAppCore 派生出的附属 service 对象`
  - `+0xd8 = 另一条 fallback manager/service 指针`
- `TPData_InetTQLAssembler` 在进入时会优先向参数对象虚表 `+0x28` 取 manager；只有参数对象不给时，才退回 `this+0xd8`。
- 因此 `0x14d349` 调用的容器虚表 `+0x60`，当前更可能挂在“job manager/service”对象上，而不是 `CAppCore` 本体上。
  - `CTdxQHztWnd::InitQHSession`
  - `CTdxUSztWnd::InitUSSession`
  - `CTdxTBztWnd::InitTBSession`
  - `CTdxZQztWnd::InitZQSession`
  - `CTdxZSztWnd::InitZSSession`
  - `CTdxQQztWnd::InitQQSession`
  - `CTdxQSztWnd::InitQSSession`
- 同时其字符串中直接出现多个 `HQServ.*` 功能名。
- 说明它掌握“高层功能名 / 市场窗口 / session”之间的映射与调度。

#### `libtpdata.so`

- 导出整套 `ITPConn_*` 会话接口：
  - `ITPConn_Init`
  - `ITPConn_LoginAnony`
  - `ITPConn_IsLogined`
  - `ITPConn_GetSession`
  - `ITPConn_SetActiveSession`
  - `ITPConn_SetUser`
- 字符串中还能看到：
  - `CTAJob_InetTQL`
- 这很像真正的 TQL 网络任务 / session 提供层。

### 4. 已确认的统一 HQ/TQL 会话建立逻辑

- 在 `tdxframe100.so` 的统一会话代码中，已直接看到如下序列：
  1. `ITPConn_IsLogined(0x65)`
  2. 若已登录，则 `ITPConn_GetSession(0x65)` 并缓存 session 指针
  3. 若未登录，则 `ITPConn_LoginAnony(0x65, hwnd, 0x41a)`
  4. 之后再次 `ITPConn_GetSession(0x65)`
- 目前可稳定确认两个关键常量：
  - session id：`0x65`
  - 登录完成消息：`0x41a`
- 这说明 Linux 版目标行情链至少包含：
  - 一个固定 session 类型
  - 匿名登录
  - 登录完成后的异步消息回调
  - 后续基于 session 的 `HQServ.*` 请求下发

### 5. 当前判断

- 目标 `7709` 新行情协议更可能属于 `TQL / ITPConn / Session` 体系。
- `libViewthem.so` 仍然保留为一条重要对照链，但不再默认视为目标 `7709` 主链。

### 6. 下一步

- 继续把 `window.external.CallTQL` 在 native 侧落到具体回调函数。
- 继续从 `ITPConn_LoginAnony(0x65, ..., 0x41a)` 追其调用者和 `0x41a` 消息处理函数。
- 继续确认 `ITPConn_GetSession(0x65)` 返回对象后，哪条路径把 `HQServ.*` 请求编码成真实网络帧。
- 用这条 `TQL / ITPConn / Session` 主链重新对照 `panzhong.pcapng` 中的 `0x1894 / 0x1899 / 0x1869` 阶段包。

### 7. 进一步修正：桥接层与处理层可能分离

- 追加搜索后可见：
  - `tcefwnd.so` 中有 `CallTQL`、`cefQuery`、`TDXQuery`，更像页面 JS 的桥接入口
  - `libViewthem.so` 中有 `CallTQL`、`CallTQLEx`、`WebCallTQL_%d_%d`、`INFO_Set_TPTQLAnswer`，更像 TQL 请求/应答处理层
  - `tdxw` 主程序字符串中还能看到：
    - `Imitate_SyncCallTQL`
    - `PsDataSyn_CallTQL_Data`
    - `CallTQL_Answer`
- 当前更合理的模块职责拆分是：
  - `tcefwnd.so`：CEF 页面桥接层
  - `libViewthem.so`：TQL 请求/应答业务处理层
  - `tdxframe100.so`：市场窗口与 session 使用层
  - `libtpdata.so`：`ITPConn_*` session 提供层
  - `tdxw`：顶层同步调用/回包分发协调层

### 8. `OnGetSession -> SendGetEvtNode -> CreateFetchDataHandle(Ex)` 已打通

- 在 `tdxframe100.so` 中，事件窗口回调 `CTdxEvtztWnd::OnGetSession` 的逻辑已经可以直接恢复：
  1. 先调用统一 session helper `0x20f5ce`
  2. 该 helper 会再次执行 `ITPConn_GetSession(0x65)`，并把结果缓存到全局 slot
  3. 若 session 有效，则给相关窗口批量 `PostMessage(..., 0x41a, ...)`
  4. `CTdxEvtztWnd::OnGetSession` 成功后立即调用 `CTdxEvtztWnd::SendGetEvtNode`
- `SendGetEvtNode` 中已能直接恢复出一组高层业务参数：
  - 功能号：`0x49b`
  - 服务名：`CWServ.tdxyj_gssj_sjzlm`
  - 参数串：`{"Params":["0","0"]}`
- `SendGetEvtNode` 最终调用统一分发函数 `0x2998e8`。

### 9. 统一分发函数 `0x2998e8` 的当前语义

- `0x2998e8` 不是单一窗口私有逻辑，而是在 `tdxframe100.so` 中被大量调用，明显是共用分发器。
- 其关键行为已经可以确认：
  - 接收调用方对象、功能号、服务名字符串、参数字符串、附加整数参数
  - 为请求构造一块临时描述结构
  - 根据环境走：
    - `CreateFetchDataHandleEx(..., 0x51f)`
    - 或 `CreateFetchDataHandle(..., 0x51f)`
  - 从 fetch handle 对象取虚表方法并发起实际请求调用
- 这说明 `tdxframe100.so` 的高层 `CWServ.* / HQServ.*` 请求，在进入真正 session / 网络层之前，会先被包装成一个 fetch handle 任务。
- 因而当前更具体的后续主链应更新为：
  - `window.external.CallTQL`
  - `tcefwnd.so` 桥接
  - `libViewthem.so` / `tdxframe100.so` 业务分发
  - `CreateFetchDataHandle(Ex)` 任务封装
  - `ITPConn session(0x65)` / 下游网络层

### 10. `CreateFetchDataHandle(Ex)` 的真实实现就在 `libtpdata.so`

- 现已确认 `CreateFetchDataHandle` / `CreateFetchDataHandleEx` / `DeleteFetchDataHandle` 都由 `libtpdata.so` 导出。
- 也就是说：
  - `tdxframe100.so` 只是 fetch 请求的使用方
  - fetch handle 的真实实现、生命周期和执行逻辑都在 `libtpdata.so`
- `libtpdata.so` 还直接导出：
  - `ITPConn_GetIAppCore`
  - `ITPConn_GetSession`
  - `ITPConn_LoginAnony`
- 这说明 fetch handle 与 `ITPConn` session 体系同属一个实现模块。

### 11. `libtpdata.so` 中的 fetch 对象骨架

- `CreateFetchDataHandle`：
  - 分配 `0x148` 字节对象
  - 进入内部构造函数
  - 随后把对象插入内部全局容器
- `CreateFetchDataHandleEx`：
  - 同样分配 `0x148` 字节对象
  - 比基础版多接收一个附加上下文参数
- `CreateBiFetchData`：
  - 分配 `0x70` 字节对象
  - 当前可作为 `IFetchData` 子对象/条目构造器看待
- `libtpdata.so` 动态符号里可见：
  - `IFetchData`
  - `CreateBiFetchData`
- 因此当前合理推断是：
  - fetch handle 是一个较大的任务对象
  - 内部维护一个或多个 `IFetchData` 条目
  - 最终再由该任务对象把请求投递到 TQL / session / 网络层

### 12. `libtpdata.so` 中已经出现默认 TQL 服务模板

- 在 `libtpdata.so` 的 rodata 中，已能直接看到：
  - `CWServ.SecuInfo`
  - `{"CallName":"%s","Params":[%s],"sso":"%s"}`
  - `{"Params":[%s],"sso":"%s"}`
- 说明 `libtpdata.so` 自身就掌握 TQL 请求字符串模板，而不仅仅是下层裸传输。
- 这进一步证明：
  - `CWServ/HQServ` 级别的请求在进入网络层之前，会在 `libtpdata.so` 内部继续被格式化和封装。

### 13. `CTAJob_InetTQL` 已与 fetch/job 解析逻辑接上

- 在 `libtpdata.so` rodata 中可见一组相邻字段：
  - `Name`
  - `JobData`
  - `CTAJob_InetTQL`
  - `Body`
- 同时在 `0x4ce23 / 0x4d03b / 0x4d284` 附近代码中，已经可以看到：
  - 先通过一组虚表调用读取对象字段
  - 再把读出的 `Name` 与 `CTAJob_InetTQL` 比较
  - 随后继续读取 `Body` 等内容并走后续处理
- 这说明 `CTAJob_InetTQL` 不是孤立字符串，而是 `libtpdata.so` 中真实参与 fetch/job 解析的一类任务名。
- 当前更具体的推断是：
  - 高层 `CWServ/HQServ` 请求先变成 fetch handle
  - fetch handle 再承载/派生出 `CTAJob_InetTQL` 任务
  - 该任务名对应真正的 “Inet TQL” 执行路径

### 14. `CTAJob_InetTQL` 的识别层与执行层已可拆开

- 目前可以把 `libtpdata.so` 里与 `CTAJob_InetTQL` 相关的代码分成两层：
  - 识别层：`0x4ce23` 与 `0x4d03b`
  - 执行层：`0x4d064` 与其下游 helper `0x4d1a0`
- `0x4ce23` / `0x4d03b` 的行为已经比较清楚：
  - 通过虚表读取字段
  - 取出 `Name`
  - 执行 `strcmp(Name, "CTAJob_InetTQL")`
- 对应 rodata 地址已可直接对上：
  - `0xa3441` -> `Name`
  - `0xa345a` -> `JobData`
  - `0xa3465` -> `CTAJob_InetTQL`
  - `0xa3471` -> `Body`

### 15. `0x4d064` 更像 job type 7 的专用分派器

- 在上游分发函数 `0x4c2e0` 中，可见一个小枚举分派：
  - 值 `3` -> 调 `0x4cef8`
  - 值 `7` -> 调 `0x4d064`
- 因而 `0x4d064` 不是单纯的字符串比较段，而是某类 job 的专用执行入口。
- `0x4d064` 内部读取的 key 已确认不是 `Name/JobData/Body`，而是另一组字段：
  - `TDX.JobType`
  - `ErrInfo`
  - `ErrType`
- 当前已可恢复的关键判定条件是：
  - `TDX.JobType == 0`
  - 上游 type id == `9`
- 只有满足这组条件时，才会继续进入后续 helper。

### 16. `0x4d1a0` 更像 `CTAJob_InetTQL` 的实例获取与填充 helper

- `0x4d1a0` 的主要动作目前可恢复为：
  1. 先从当前上下文中获取或回退到一个会话/任务容器对象
  2. 调容器虚表 `+0x70`，传入常量 `5` 和字符串 `CTAJob_InetTQL`，获取一个任务对象
  3. 若对象有效，则通过对象虚表 `+0x8` 写入：
     - `Name`
     - `Body`
  4. 最后调容器虚表 `+0x60`，把已填充的 job 对象挂回容器
- `Body` 的默认兜底值可直接看到是字符串 `0`。
- 这说明 `CTAJob_InetTQL` 在这里已经进入“实例化并提交”的阶段，而不只是被识别的任务名。

### 17. 当前最值得继续深挖的点

- `0x4d349`：容器虚表 `+0x60` 的调用点，像是把 `CTAJob_InetTQL` job 正式提交给下一层执行器
- `0x4d284`：容器虚表 `+0x70` 的调用点，像是按任务名获取/创建 job 实例
- `0x37a56` 与 `0x4d406`：这两处也会调用 `0x4d1a0`，说明该 helper 不是单一路径私有，后续需要区分它们对应的上游请求

### 18. `0x3e924` 构造的是 fetch handle 本体，不是 `0x4d1a0` 使用的 job 容器

- 继续回看 `CreateFetchDataHandle` / `CreateFetchDataHandleEx` 后可确认：
  - 两者都直接 `new 0x148`
  - 然后调用 `0x3e924`
- `0x3e924` 写入的 vtable 地址点是：
  - `0x2ddd98`
  - `0x2dded8`
- 这组 vtable 的前几项方法明显在处理 fetch handle 自身状态，而不是 job 容器：
  - 析构时会释放 `+0x100` 指针和 `+0x110` 附近缓冲区
  - 方法会访问本对象字段：
    - `+0x138`
    - `+0x13a`
    - `+0x13c`
    - `+0x140`
    - `+0x144`
    - `+0x147`
- 同时这些方法会拼接或操作一组结果字段名：
  - `ErrorInfo`
  - `ResultSets`
  - `Content`
  - `ColDes`
- 因而当前应明确区分两类对象：
  - fetch handle：由 `0x3e924` 构造，大小 `0x148`
  - job/manager 容器：由 `0x4d064 / 0x4d1a0` 使用，拥有 `+0x500 / +0x508 / +0x530 / +0x538` 一带的大对象布局

### 19. `0x4c20c` 是 job 分派入口，`0x4d064` 只是其中一个分支

- `0x4c20c` 会先对输入对象调用虚表 `+0xb0`，填充一个本地 `0x18` 字节描述块。
- 随后再通过虚表字段名 `TDX.JobType` 取出一个整型 job type，并按枚举分派：
  - 值 `2` -> `0x4c830`
  - 值 `3` -> `0x4cef8`
  - 值 `5` -> `0x4ccb4`
  - 值 `7` -> `0x4d064`
- 这说明 `CTAJob_InetTQL` 路径不是独立入口，而是整个 job 分派器中的一条专门分支。

### 20. `0x4d1a0` 的两个已知上游，分别对应 `std::string` 和 C 字符串包装

- `0x37a56` 调用 `0x4d1a0` 前，会从对象 `+0xb0` 处取 `std::string` 的 `length()` 与 `data()`，再把：
  - `data`
  - `len`
  - 以及对象内另一段上下文地址
  一起传给 `0x4d1a0`
- `0x4d406` 则是一个更直接的包装器：
  - 对传入 `char*` 先做 `strlen`
  - 再把 `ptr + len` 交给 `0x4d1a0`
- 这说明 `0x4d1a0` 的核心职责是：
  - 接受任意来源的 body 文本
  - 将其装配为 `CTAJob_InetTQL`
  - 再挂回更大的 job/manager 容器

### 21. `type 2 / 3 / 5 / 7` 四条分支的角色已可初步分层

- 当前从 `0x4c20c` 往下看，四条已知分支的角色已经不再等价：
  - `type 2 -> 0x4c830`
  - `type 3 -> 0x4cef8`
  - `type 5 -> 0x4ccb4`
  - `type 7 -> 0x4d064`

### 22. `type 2 -> 0x4c830` 更像 `ACL:checkuser` / 消息通知链

- `0x4c830` 开头会先构造一个本地描述对象，再调用 `0x4d59a` 做匹配/查找。
- 随后它继续从 job 对象中读取：
  - `ErrorInfo`
  - `ErrInfo`
  - `ErrType`
- 如果 type id 为 `0x10`，还会读取 `Name`，并与字符串 `ACL:checkuser` 比较。
- 在命中特定条件后，这条路径最终会走到 `PostMessageA`。
- 因而当前更合理的判断是：
  - `type 2` 不是目标 `7709` 行情发包链
  - 更像登录检查、ACL 校验或错误通知回投到窗口消息队列的路径

### 23. `type 5 -> 0x4ccb4` 已经直接消费 `JobData`

- `0x4ccb4` 一开始就从 job 对象读取 `JobData`。
- 取出文本后，直接调用 `0x4d406`。
- `0x4d406` 本身只是一个 C 字符串包装器：
  - 先 `strlen`
  - 再把 `ptr + len` 交给 `0x4d1a0`
- 这说明 `type 5` 路径的核心语义是：
  - 把 `JobData` 文本进一步装配为 `CTAJob_InetTQL`
- 此外，`type 5` 在 type id 为 `0x10` 且 `ErrType != 0` 时，还会读取 `ErrorInfo` 并调用回调完成接口。

### 24. `type 3 -> 0x4cef8` 更像 `CTAJob_InetTQL` 的直接完成回调链

- `0x4cef8` 会读取：
  - `TDX.JobType`
  - `ErrInfo`
  - `ErrType`
- 当 `TDX.JobType == 0` 且 type id 为 `0x10` 时，它会继续读取：
  - `Name`
  - `Body`
- 若 `Name == CTAJob_InetTQL`，则直接调用回调对象虚表：
  - 一次把 `Body` 和整型参数交给回调
  - 再一次发完成通知
- 因此 `type 3` 更像：
  - 某个 `CTAJob_InetTQL` 任务已执行完成后的结果回调链
  - 而不是组包/提交链的最前端

### 25. 当前最接近“文本 job -> 内部 InetTQL 任务装配”的是 `type 5 + type 7`

- 结合目前已见到的行为：
  - `type 5`：从 `JobData` 文本进入 `0x4d406 -> 0x4d1a0`
  - `type 7`：从 manager/job 容器继续走 `0x4d064 -> 0x4d1a0 -> 容器提交`
- 当前最合理的分层是：
  - `type 5` 更像“把文本 job 数据灌入 `CTAJob_InetTQL` 装配器”的入口层
  - `type 7` 更像“manager 容器里真正执行/提交这类 job”的下一层
- 因而如果目标是继续逼近 `7709` 二进制行情帧生成位置，优先级应继续放在：
  - `0x4d349`
  - `0x4d284`
  - `0x4ccb4` 的上游来源

### 26. `0x4d064/0x4d1a0` 使用的大对象，已可定位到一个消息窗口类

- 继续追 `+0x500/+0x530/+0x538` 布局后，可恢复出一段明确的构造代码：`0x4b300`。
- `0x4b300` 的关键初始化动作包括：
  - `+0x98 = 0`
  - `+0xd8 = 0`
  - `+0x500 = 0`
  - `+0x508` 初始化为 `IFetchData*` 容器
  - `+0x530 = 0`
  - `+0x538` 初始化为另一段内部容器/索引结构
- 同时它还会：
  - 初始化窗口相关基类 `CWnd`
  - 注册窗口类
  - 使用类名字符串 `TPData_MsgWnd`
- 这说明 `0x4d064 / 0x4d1a0` 所在的大对象，不只是抽象 manager，而是一个带窗口消息泵的本地消息窗口对象。

### 27. `+0x500` 不是普通字段，而是消息窗口懒加载出的外部执行器指针

- 在 `0x4b66a` 这一段逻辑中，可看到：
  - 若 `this + 0x500` 为空，则通过全局函数指针创建一个对象并写入 `+0x500`
  - 随后立刻调用该对象虚表 `+0x20`，把 `this + 0xe0` 传进去
  - 若 `this + 0x98` 仍为空，再调用该对象虚表 `+0x158`，并把返回值写入 `+0x98`
- 因而当前更合理的结构解释是：
  - `TPData_MsgWnd` 自己持有：
    - 一个外部执行器/会话对象指针 `+0x500`
    - 一个由该执行器派生出的附属对象 `+0x98`
  - `0x4d064` 中反复使用的 `+0x500`，很可能正是后续提交 `CTAJob_InetTQL` 的关键执行器

### 28. `2ee1e0 / 2ee1e8` 已确认是 `dlsym` 解析出的动态工厂函数

- 在 `0x45ac7` 和 `0x45aed`，可直接看到：
  - 从一个 `dlopen` 得到的模块句柄上做 `dlsym`
  - 把结果分别写入全局：
    - `0x2ee1e0`
    - `0x2ee1e8`
- 后续 `0x4b66a` 会直接使用 `0x2ee1e0` 指向的函数来创建 `+0x500` 对象。
- 而 `0x4c19a / 0x4c1a9` 则会用 `0x2ee1e8` 指向的函数去释放 `+0x500` 对象。
- 因而当前可以把这两个全局解释为：
  - `0x2ee1e0`：外部执行器创建函数
  - `0x2ee1e8`：外部执行器销毁函数

### 29. 当前主链进一步收敛

- 现在主链可继续细化为：
  - 高层 TQL / job 文本
  - `type 5 -> 0x4ccb4`
  - `0x4d406 -> 0x4d1a0` 装配 `CTAJob_InetTQL`
  - `type 7 -> 0x4d064`
  - `TPData_MsgWnd` 持有的外部执行器 `+0x500`
  - 外部执行器虚表方法
  - 再往下才是 session / 网络执行层
- 这意味着下一步最有价值的点已经更新为：
  - `0x4b66a` 中 `+0x500` 对象的虚表 `+0x20` 和 `+0x158`
  - `0x4d349` 提交时实际命中的执行器方法

### 30. `+0x500` 外部执行器已经坐实为 `libtaapiw.so` 的 `AppCore`

- 现已确认：
  - 模块：`libtaapiw.so`
  - 创建函数：`TaApi_CreateAppCore`
  - 销毁函数：`TaApi_DestroyAppCore`
- `TPData_MsgWnd` 在 `0x4b66a` 中用 `0x2ee1e0` 创建 `+0x500` 对象，这个全局函数指针正是通过 `dlsym` 解析出的 `TaApi_CreateAppCore`。
- 同理，`0x2ee1e8` 对应 `TaApi_DestroyAppCore`，用于在窗口销毁链中释放 `+0x500`。

### 31. `TaApi_CreateAppCore` 返回的是一个 `0x318` 大小的 `CAppCore`

- `libtaapiw.so:0x1823a0` 的 `TaApi_CreateAppCore` 行为已经很直接：
  - `new 0x318`
  - 调内部构造函数 `0x132be0`
  - 返回对象指针
- `0x132be0` 一进入就把对象 vptr 写到：
  - `0x786918`
- 同时字符串和源码路径中可见：
  - `CAppCore`
  - `IAppCore`
  - `AppCore.cpp`
- 因而当前可以把 `+0x500` 的对象明确命名为：
  - `CAppCore` 实例

### 32. `CAppCore` 内部明确带有 SessionManager / Session / CTAJob_InetTQL 语义

- `libtaapiw.so` 字符串中已可直接看到：
  - `CTAJob_InetTQL`
  - `CTAJob_RPCSessionKey`
  - `CTDXSession CreateJob`
  - `CTDXSession InExecute`
  - `CTDXSession RevcJob`
  - `CTDXSession ConnectIn`
  - `CTDXSession ConnCpl`
  - `CTDXSession CommitLoginSuccess`
  - `PushTQL`
  - `ISessionManager`
  - `ISession`
  - `IEventHook`
  - `IMBClient`
- 这说明：
  - 一旦进入 `libtaapiw.so` 的 `CAppCore`
  - 其内部已经拥有真正的 SessionManager / CTDXSession / CTAJob_InetTQL 执行语义
  - 离 `7709` 新行情协议的网络执行层只剩 AppCore 内部几层对象调用

### 33. `TPData_MsgWnd` 命中的 `CAppCore` vtable 槽位已可精确定位

- `CAppCore` 构造函数写入的 vtable 起点是 `0x786918`。
- 因而 `TPData_MsgWnd` 在 `0x4b66a` 中调用的两个关键槽位目前可精确换算为：
  - vtable `+0x20` -> `libtaapiw.so:0x132f30`
  - vtable `+0x158` -> `libtaapiw.so:0x137e40`

### 34. `vtable +0x20 (0x132f30)` 更像把 `TPData_MsgWnd` 的 hook/context 注册进 `CAppCore`

- `0x132f30` 的实参关系与上游现场是对得上的：
  - `rdi = CAppCore*`
  - `rsi = TPData_MsgWnd + 0xe0`
- 进入后它会：
  - 把第二参数保存到 `this + 0x8`
  - 随后围绕该对象调用若干虚方法和字符串/对象包装函数
- 结合字符串：
  - `IEventHook`
  - `IMBClient`
  - `m_pISessionMag!=NULL&&pIEventHook!=NULL&&pIMBClient!=NULL`
- 当前更合理的判断是：
  - `0x132f30` 不是网络提交函数
  - 更像把 `TPData_MsgWnd` 提供的 hook / client / 回调上下文注册给 `CAppCore`

### 35. `vtable +0x158 (0x137e40)` 的旧判断已过期；它后来被真实反汇编改写为“带保护的 `+0x18` 访问器”

- 这一节保留仅用于记录早期推断轨迹。
- 后续真实反汇编已经证明：
  - `0x137e40` 先取宿主对象 `+0x18`
  - 若为空则走统一日志/诊断路径
  - 最终仍返回宿主对象 `+0x18`
- 因而本节原先关于“懒取内部服务对象”的判断，当前应以第 95、99、100 节为准，不再单独采信。

### 36. 当前最关键的边界已经跨过

- 到目前为止，链路已经从：
  - `libtpdata.so` 中抽象的 fetch/job/manager 分支
- 走到了：
  - `TPData_MsgWnd`
  - `CAppCore`
  - `CTDXSession`
  - `CTAJob_InetTQL`
- 这意味着后续逆向的主战场已经应从 `libtpdata.so`，部分转移到 `libtaapiw.so` 内部：
  - `CAppCore`
  - `SessionManager`
  - `CTDXSession`
  - `CTAJob_InetTQL`

### 37. `libtaapiw.so` 中的 `CTDXSession` 状态机函数已可按日志字符串命名

- 通过 `rodata` 与交叉引用，现在已经能把一组 `CTDXSession` 函数和日志串一一对应起来：
  - `0x17acb0`：`CTDXSession CreateJob Session=%p,Client=%p,Event=%d,State=%d,Job=%p`
  - `0x17ada0`：`CTDXSession InExecute Session=%p,Client=%p,Event=%d,State=%d,Job=%p`
  - `0x17af00`：`CTDXSession RevcJob Session=%p,Client=%p,Event=%d,State=%d,Job=%p`
  - `0x17b020`：`CTDXSession DisConnCpl Session=%p,Client=%p,Event=%d,State=%d,Job=%p`
  - `0x17ed80`：`CTDXSession SetOpt Session=%p,Client=%p,Key=%s`
  - `0x17ef20`：`CTDXSession ConnectIn Session=%p,Client=%p,Event=%d,State=%d,Job=%p`
  - `0x17f280`：`CTDXSession ConnCpl Session=%p,Client=%p,Event=%d,State=%d,Job=%p`
- 这说明此前已经确认进入的 `CAppCore -> SessionManager -> CTDXSession` 不是抽象语义，而是已经落到带事件号、状态值、job 指针的真实状态机函数体。

### 38. `CreateJob / InExecute / RevcJob` 这三段当前可恢复的语义

- `0x17acb0` (`CreateJob`)：
  - 参数形态与日志完全一致：`this/session`、`event`、`state`、`job/client`。
  - 真正业务动作很短，核心是把 `this + 0x550` 这段内部同步对象拷到栈临时区，再清零 `this + 0x80`。
  - 因而它更像“进入某个 job 事件前的轻量包装/登记函数”，不是最终网络执行点。
- `0x17ada0` (`InExecute`)：
  - 先对 `this + 0x550` 做加锁/时间戳更新，写入 `this + 0x464`。
  - 然后对传入 job 对象调用虚表 `+0x30`。
  - 当 `event == 0x0c` 时，还会额外调用 job 对象虚表 `+0x0`。
  - 这更像 `CTDXSession` 把 job 推入执行阶段的统一入口，真正 job-specific 的行为已经下沉到 job 对象虚表。
- `0x17af00` (`RevcJob`)：
  - 会把 `event/state/job` 组合进本地描述块，再调用 `this` 的虚表 `+0x100`。
  - 随后同样经过 `this + 0x550` 的同步对象清理，最后清零 `this + 0x80`。
  - 这表明 `RevcJob` 更像 session 内部的“收到 job/事件后转交虚表处理”的桥接层。

### 39. `ConnectIn / ConnCpl / DisConnCpl` 已暴露出连接阶段状态字段

- `0x17ef20` (`ConnectIn`)：
  - 首先通过回调对象读取两个关键字段：
    - `6055c1` 对应的键，结果写入栈上的整型槽位
    - `6147c6` 对应的键，结果写入另一处指针槽位
  - 若前者非 0，则进入一条“已有连接结果”的路径：
    - 更新时间戳 `this + 0x4d4`
    - 若 `this + 0x458 != 0`，则复制 `this + 0x452 -> this + 0x450`
    - 调 `0x177e80(this, 5)`
    - 再经回调对象写回一组结果字段
  - 若前者为 0，则进入真正“发起连接”的路径：
    - 通过 `this + 0x68` 的接口对象写入多个配置字段到 `this + 0x184`、`this + 0x204`、`this + 0x580`
    - 设置 `this + 0x3f8 = 1`、`this + 0x3fc = 1`
    - 清零 `this + 0x450`，清空 `this + 0x4dc`
    - 更新时间 `this + 0x464`
- `0x17f280` (`ConnCpl`)：
  - 直接打印 `ConnCpl` 日志后跳回 `0x17ef5d`，也就是 `ConnectIn` 的主逻辑入口。
  - 说明 `ConnCpl` 本身更像连接完成事件壳层，真正状态迁移复用了 `ConnectIn` 里的主状态机。
- `0x17b020` (`DisConnCpl`)：
  - 主要做状态复位：
    - `this + 0x3f8 = 0`
    - `this + 0x404 = 0`
    - `this + 0x4d4 = time()`
    - `this + 0x450 = 0`
    - `this + 0x80 = 0`
  - 这条路径很像断连完成后的 session 清理逻辑。

### 40. `CTAJob_InetTQL` 与 `PushTQL` 当前看到的是对象层，不是最终 socket 组包点

- `0x00c7f00` 使用日志串 `CTAJob_InetTQL name=%s`：
  - 该函数操作的核心字段在对象 `+0x548` / `+0x558` / `+0x530` / `+0x538` 一带。
  - 先从 `+0x548` 取名字/文本，再从 `+0x558` 取一个子对象做解析，接着依据结果更新 `+0x498/+0x4c8/+0x4d0`。
  - 若 `+0x530` 非空，则通过函数指针和 `+0x538` 再做一次对象级分发。
  - 说明这里更像 `CTAJob_InetTQL` 的接收/解析/回调层，而不是直接发 7709 二进制帧。
- `0x00c50b2` 使用日志串 `CTAJob_InetTQL<0x%p>:   Recv Fragment=%d, Data Size=%u`：
  - 这段逻辑遍历链表样结构，逐个 fragment 取长度与数据，再把结果提交给 `+0x530/+0x538` 关联的处理器。
  - 因而它更接近“分片接收后的重组与上抛”，仍然偏向收包侧。
- `PushTQL` 位于一组 `Push*` 关键字里：
  - `PushKickOut`
  - `PushIX`
  - `PushTQL`
  - `PushTJS`
  - `PushBody`
  - `PushFallDown`
  - `PushCmdDesc`
- `0x126713` 对 `PushTQL` 的引用方式不是网络调用，而是把输入字符串与多个 `Push*` 关键字逐个比较，再把对象字段写入一个序列化缓冲区。
- 因而当前更合理的判断是：
  - `PushTQL` 属于 RPC/TQL 结构体字段编解码层
  - 还不是连接 `xxx:7709` 后的最终 socket 发送点

### 41. 当前最可信的继续下钻方向

- 由于 `CTDXSession InExecute` 最终把 job-specific 行为交给了 job 对象虚表，而 `ConnectIn/ConnCpl` 暴露的更多是连接状态字段，当前最值得继续追的点变成：
  - `0x17ada0` 中 job 对象虚表 `+0x30`
  - `0x17af00` 中 session 对象虚表 `+0x100`
  - `0x17ef20` 中调用的 `0x177e80(this, 5)`
  - `CTDXSession CommitLoginSuccess`、`CTDXSession Connect`、`CTDXSession Exit` 这些同簇日志对应函数
- 只有把这些虚表槽位和下游对象类型继续钉住，才更可能把链路从 `CTDXSession` 推进到真正的 `7709` 协议编码/发包函数。

### 42. `0x177e80` 不是网络层，而是“临时切换状态并调用公共处理器”的包装器

- `0x177e80` 的行为已经比较清楚：
  - 先保存 `this + 0x20` 的旧值
  - 把传入的 `esi` 临时写到 `this + 0x20`
  - 调公共处理器 `0x177a40(this, esi)`
  - 返回前再把旧值恢复到 `this + 0x20`
- 因而 `ConnectIn` 中的 `0x177e80(this, 5)` 并不是“直接发起网络连接”，而更像：
  - 以临时状态码 `5` 进入一段共用处理逻辑
  - 执行完后恢复原会话状态
- 另一处 `0x17c398` 也会调用 `0x177e80(this, 5)`，说明这个包装器不是 `ConnectIn` 私有逻辑，而是被多个 session 状态路径复用。

### 43. `CTDXSession` 还存在 `InNotify / GeneralCL RunTimeJob / Init Session` 等同簇处理器

- 通过更多日志字符串交叉引用，当前还能稳定命名出几条同簇函数：
  - `0x17b180` -> `CTDXSession InNotify Session=%p,Client=%p,Event=%d,State=%d,Job=%p`
  - `0x17c7c0` -> `CTDXSession GeneralCL RunTimeJob Session=%p,Client=%p,Event=%d,State=%d,Job=%p`
  - `0x17cbc0` -> `CTDXSession Init Session=%p,Client=%p`
- 其中：
  - `0x17b180 (InNotify)` 的外形与 `RevcJob` 很接近：也是组本地描述块，再经 session 虚表 `+0x100` 下沉，说明 `+0x100` 很可能就是 `CTDXSession` 对外分发事件/任务的核心统一槽位。
  - `0x17c7c0 (GeneralCL RunTimeJob)` 明确拿到了一个 job 对象，并调用其虚表 `+0x30` 以及 `+0x0`，这进一步坐实：真正 job-specific 的执行语义在 job 对象，而不是 CTDXSession 壳层。
  - `0x17cbc0 (Init Session)` 当前看到的日志段本身较薄，但它与后面的 `0x17cc50` 初始化逻辑直接相邻，明显属于 session 初始化簇。

### 44. `0x17cc50` 很像 `CTDXSession` 的分发表注册入口

- `0x17cc50` 的行为非常关键：
  - 先检测某个全局初始化条件
  - 随后把一批函数地址写入一组连续全局槽位
- 当前已经能直接从这段里恢复出的映射包括：
  - `CreateJob -> 0x17acb0`
  - `InNotify -> 0x17b180`
  - `InExecute -> 0x17ada0`
  - `RevcJob -> 0x17af00`
  - `ConnectIn -> 0x17ef20`
  - 以及若干仍待补名的入口：`0x17ebc0`、`0x17c440`、`0x17f710`
- 这说明 `CTDXSession` 不是靠零散直接调用驱动，而是：
  - 先初始化一张事件/状态处理分发表
  - 再由上层按 event/state/job 类型把请求分派到具体处理器
- 这类结构对后续逆向很重要，因为：
  - 不再需要只靠全文搜索日志或字符串硬找调用链
  - 可以直接把分发表中的尚未命名函数逐个补齐，系统性还原整个 session 状态机

### 45. 当前对“最接近 7709 发包点”的判断进一步收敛

- 目前已经可以排除以下几类点不是最终目标：
  - `0x177e80`：只是状态切换包装器
  - `CTDXSession CreateJob / InExecute / RevcJob / InNotify`：主要是状态机壳层和统一分发层
  - `CTAJob_InetTQL name / Recv Fragment` 两处：更偏收包解析/分片重组
  - `PushTQL`：更偏 RPC/TQL 字段编解码层
- 当前最接近真实发包链的点，已经收敛到：
  - `0x177a40` 公共处理器
  - session 虚表 `+0x100` 命中的实际实现
  - job 虚表 `+0x30` 命中的实际实现
  - `0x17cc50` 分发表里仍未命名的 `0x17c440 / 0x17ebc0 / 0x17f710` 等函数
- 下一步如果要继续逼近 `7709` 的 `0x1894 / 0x1899 / 0x1869` 二进制帧，应该优先从这些“分发表落点”和“job 虚表落点”继续往下，而不是再停留在 `CTDXSession` 的外层日志函数上。

### 48. `libTdxAsioComm.so` 已可保守落名为 `CUserComm` 通信对象层

- `MakeUserCommModule (0x3b2e0)` 只是薄工厂：
  - 取全局互斥锁
  - `new 0xa0`
  - 调内部构造 `0x22ee0`
- `DelUserCommModule (0x3b350)` 也是薄释放器：
  - 取全局互斥锁
  - 对外部传入的对象指针调用内部析构 `0x22d40`
  - `delete`
- `0x22ee0` 构造函数写入的 vtable 对应 typeinfo 名字已经能直接和 rodata 对上：
  - vtable 位于 `0x24e510`
  - typeinfo 位于 `0x24e938`
  - typeinfo 指向的名字字符串位于 rodata `0x41180`
  - 字符串内容是 `9CUserComm`
- 因而当前可以保守把这组函数落名为：
  - `0x22ee0 -> CUserComm_Construct`
  - `0x22d40 -> CUserComm_Destroy`

### 49. `CUserComm` 的对象骨架已经比之前清楚很多

- `CUserComm` 本体大小是 `0xa0`。
- 构造函数里能直接看见它内嵌/缓存了几类关键成员：
  - `this+0x18` 一带的 resolver/io 对象
  - `this+0x40` 一带的 `reactive_socket_service<tcp>` 相关实现
  - `this+0x48`：socket fd，初始化为 `-1`
  - `this+0x4c`：socket 标志字节
  - `this+0x50`：挂起节点/挂起操作对象
  - `this+0x60`：executor/impl 对象
  - `this+0x68`：一个启用/拥有 executor 的布尔位
  - `this+0x70`：上层回调函数指针
  - `this+0x90`：连接/停止相关状态标志
  - `this+0x98`：`deadline_timer` 指针
- 这说明 `libTdxAsioComm.so` 已不是抽象 manager，而是真正持有 socket、resolver、timer、callback 的网络会话对象层。

### 50. `CUserComm` 的几条关键方法已经可以按职责落名

- `0x21d70`：会把 `std::thread` 的 state 绑定到 `CUserComm::Run(void)`，循环创建并 `detach` 工作线程；可保守命名为 `CUserComm_StartWorkers`。
- `0x21ec0`：就是上面线程 state 绑定进去的目标函数；它本体很薄，核心是驱动 `this+0x10` 上的 io/context 继续运行；可保守命名为 `CUserComm_Run`。
- `0x21f10`：
  - 直接构造 `msghdr/iovec`
  - 调 `sendmsg@plt`
  - 失败时会经 `poll@plt` 做等待与重试
  - 对大于 `0xffff` 的缓冲区做分段发送
  - 当前可保守命名为 `CUserComm_SendRaw`
- 这使得当前对“7709 二进制帧真正从哪里离开用户态”的判断明显收敛：
  - 更高层在 `libtpdata.so/libtaapiw.so` 组装任务与状态
  - 真正出站已经逼近到 `CUserComm_SendRaw -> sendmsg`

### 51. `CUserComm` 的连接与超时路径也已初步坐实

- `0x23ee0`：
  - 把传入的 host/port 构造成 `std::string`
  - 使用 `this+0x18` 上的 resolver 做 `resolve`
  - 再把结果交给 `0x23180` 一带的下游 helper
  - 当前适合保守命名为 `CUserComm_ResolveAndConnect`
- `0x26380`：
  - 同样先 resolve host/port
  - 把上层回调函数指针缓存到 `this+0x70`
  - 清 `this+0x90`
  - 销毁旧的 `this+0x98` timer（如果存在）
  - 新建 `deadline_timer` 到 `this+0x98`
  - 用传入超时值乘 `1000` 后设置过期时间
  - 再挂接回调 `0x22450`
  - 当前适合保守命名为 `CUserComm_ConnectWithTimeout`
- `0x22450` 的行为已经比较明确：
  - 是一个统一的状态回调分发器
  - `type 1/2/3` 三类分支分别处理错误、取消/超时、连接完成
  - 最终经 `this+0x70` 的回调函数指针，把错误码和字符串上抛给上层
  - 它引用的 rodata 字符串里已能看到 `out of time`、`write`、`read`、`close`、`connect`
  - 因此当前适合保守命名为 `CUserComm_DispatchStatusCallback`

### 52. 当前对 `7709` 发包层的收敛结果

- 以前只能说 `libTdxAsioComm.so` 是 `boost::asio` 通信层；现在已经能进一步具体到：
  - 工厂 `MakeUserCommModule` 返回的真实对象就是 `CUserComm`
  - `CUserComm` 自己持有 resolver/socket/timer/callback
  - 连接入口已经出现为 `ResolveAndConnect` / `ConnectWithTimeout`
  - 真正的原始出站点已经落到 `CUserComm_SendRaw -> sendmsg`
- 因而后续如果继续逼近 `0x1894 / 0x1899 / 0x1869` 这些帧：
  - 一个方向是从 `libtpdata/libtaapiw` 继续往下找谁把最终二进制缓冲区传给 `CUserComm_SendRaw`
  - 另一个方向是继续沿 `CUserComm_ConnectWithTimeout -> callback -> read/write handler` 把收发回调全部补名

### 53. `CUserComm` 的 vtable 已能把连接/读接口固定到具体槽位

- `CUserComm` vtable 在 Ghidra 地址 `0x34e510`，当前前 15 个槽位已能稳定对上关键接口：
  - `[02] -> 0x126380 -> CUserComm_ConnectWithTimeout`
  - `[06] -> 0x121d70 -> CUserComm_StartWorkers`
  - `[08] -> 0x123ee0 -> CUserComm_ResolveAndConnect`
  - `[10] -> 0x121f10 -> CUserComm_SendRaw`
- 这说明 `SendRaw` 不是单纯的内部 helper，而是 `CUserComm` 公开接口面上的正式槽位之一。
- 结合这张 vtable，再看未命名槽位的函数体，已经可以把一组高置信接口继续收敛出来：
  - `0x121600`：主要围绕 `this+0x10` 子对象和停止标志做互斥清理，适合保守命名为 `CUserComm_ClearStoppedState`
  - `0x1222b0`：循环调用底层读取 helper，分段累计长度，适合保守命名为 `CUserComm_Read`
  - `0x1223c0`：单次读取，直接命中 `read_some` 语义，适合保守命名为 `CUserComm_ReadSome`
  - `0x124730`：零超时直读，非零超时走 `deadline_timer + cancel` 竞争，适合保守命名为 `CUserComm_ReadSomeWithTimeout`
  - `0x1240f0`：调用 `gethostname` / `inet_ntop` 拼接文本结果，更像 `CUserComm_GetLocalHostAddrs`
  - `0x1272c0`：先 `resolve` 再按超时分支走等待式连接，适合保守命名为 `CUserComm_ResolveConnectBlocking`
- 当前仍不宜过早命名的槽位主要是 `0x124e70 / 0x125120 / 0x125290 / 0x1255d0 / 0x128220`：
  - 它们已经明显属于 callback/timer 包装层或读写变体
  - 但现阶段还不足以把它们严格定死为 `Write`、`Close` 或某一条唯一的异步入口
- 对当前主目标的意义是：
  - `CUserComm_SendRaw -> sendmsg` 依然是最靠近 `7709` 原始出站的锚点
  - 但现在已经能确定，`CUserComm` 除了连接接口外，还向上层显式暴露了 `Read / ReadSome / ReadSomeWithTimeout` 这组数据面接口
  - 因而从 `libtpdata/libtaapiw` 往下追时，除了找谁最终调用 `SendRaw`，也应同时注意谁持有并消费这组读接口槽位

### 54. `CUserComm` 的剩余未命名槽位里，已经能分离出 connect callback 面和 timer/op 包装面

- 对 `0x128220` 的函数体导出后，现在可以比较稳地把它和 `0x126380/0x1272c0` 区分开：
  - 入口同样先把 host/port 文本包装成两段 `std::string`
  - 然后通过 `resolve` 产出结果集
  - 把上层回调缓存到 `this+0x70`
  - 再以 `type=3`、回调函数 `0x122450`（即 `CUserComm_DispatchStatusCallback`）构造连接请求并下发
  - 全程不创建 `this+0x98` 的 `deadline_timer`
- 因而 `0x128220` 当前更适合保守命名为 `CUserComm_ConnectAsync`：
  - 它比 `CUserComm_ConnectWithTimeout` 少了 timer
  - 又比 `CUserComm_ResolveConnectBlocking` 更明显地走 callback/异步对象链

- `0x125120` 的函数体也已经比较清楚：
  - 会把传入对象缓存到 `this+0x78`
  - 当超时值非零时，先做 `timeout * 1000`
  - 然后对一条 `io_object_impl` 路径调用 `expires_from_now`
  - 再以 handler `0x121300` 和 `type=2` 构造后续 wait/timer 请求
  - 出错字符串直接命中 rodata `"expires_from_now"`
- 这说明 `0x125120` 更像“启动 deadline/timer wait”的包装层，而不是最终读写主体。

- `0x124e70` 与 `0x125290` 则更像一对异步 op 包装：
  - `0x124e70` 会把调用方对象缓存到 `this+0x88`
  - `0x125290` 会把调用方对象缓存到 `this+0x80`
  - 两者都会构造独立的 op 对象，并克隆 `this+0x60/+0x68` 这组 executor 状态
  - 区别在于：
    - `0x124e70` 直接下发 handler `0x1214a0`
    - `0x125290` 在需要时先 `expires_from_now`，再下发 handler `0x1213d0`
- 因而当前更合理的理解是：
  - `0x124e70` 更像无超时的异步 I/O 包装
  - `0x125290` 更像带超时的异步 I/O 包装
  - 但在没有把它们和具体 `read/write/close` 字符串或上游调用点完全对死前，还不宜把名字过早落成 `Write` 或 `Close`

- 这一轮最重要的增量不是再多落几个名字，而是把 `CUserComm` 剩余槽位分成了三层：
  - `+0x70`：连接/状态回调面，已由 `CUserComm_ConnectAsync` 与 `CUserComm_DispatchStatusCallback` 固定下来
  - `+0x78`：deadline/timer wait 面，对应 `0x125120`
  - `+0x80/+0x88`：异步 op 包装面，对应 `0x125290 / 0x124e70`
- 这意味着后续如果继续往 `7709` 原始出站逼近，最值得追的已经不是“还有没有 connect 入口”，而是：
  - 谁在上层实际持有并调用 `+0x80/+0x88` 这两组异步 op 包装
  - 以及它们最终是否会在某一支内部调用 `CUserComm_SendRaw`

### 54.1 `0x124e70 / 0x125290` 的下游已经能分辨成“非阻塞 descriptor 提交”和“reactor/timer 提交”

- 继续把 `0x124e70` 往下拆后，它的关键下游已经能固定到 `0x132a00`：
  - 在 `0x132a45` 之后直接调用 `ioctl`
  - ioctl 常量是 `0x5421`
  - 配套栈值先被写成 `1`
  - 这在 Linux 上对应的就是 `FIONBIO`
- 也就是说，`0x124e70` 不是普通的 connect 壳层，而是在真正把 descriptor 交给异步框架前，先确保 fd 被切成非阻塞模式；随后才把 op 交给 `0x12dfb0` 这条更深的 reactor 路径。
- 因而当前对 `0x124e70` 的更稳理解应当是：
  - 它属于 `CUserComm` 暴露给上层的无超时异步 descriptor 提交槽位
  - 还不能直接把它命成 `Write` 或 `Read`
  - 但已经可以明确排除“又一个 connect 入口”的可能

- `0x125290` 往下则能固定到 `0x136550`：
  - 该函数会显式读取调用方对象上的 timer/queue 状态位
  - 在不同分支里操作 `epoll_ctl`
  - 还会调用 `timerfd_settime`
  - 并命中 Boost.Asio `timer_queue` 的 `_M_realloc_insert<...heap_entry...>`
- 这说明 `0x125290` 的本体也不是具体业务编码器，而是把异步 op 连同超时条件一起挂进 reactor/timer 队列。
- 因而当前对 `0x125290` 的更稳理解应当是：
  - 它是 `CUserComm` 的带超时异步提交槽位
  - 负责把 op 和 timer 绑定后交给底层 event loop
  - 仍然不能仅凭这一层就把它定名成 `WriteWithTimeout`

- 结合 `0x34e518..0x34e568` 的 vtable 切片，现在 `CUserComm` 这一段接口面已经可以按顺序排成：
  - `+0x28` 附近：`CUserComm_ConnectAsync`
  - `+0x30` 附近：`CUserComm_ConnectWithTimeout`
  - `+0x38`：`0x125120` deadline/timer wait
  - `+0x40`：`0x125290` 带超时异步提交
  - `+0x48`：`0x124e70` 无超时异步提交
  - 后续继续是 `StartWorkers / ClearStoppedState / ResolveAndConnect / ResolveConnectBlocking / SendRaw / Read`
- 这轮最关键的收敛点是：
  - `SendRaw` 仍然是唯一明确命中 `sendmsg` 的出站锚点
  - `0x124e70 / 0x125290` 现在已经能被归入“把某类 descriptor op 提交给 reactor”的接口层
  - 后续真正值得追的是：谁在 `libtpdata/libtaapiw` 上层调用了这两个槽位，以及这些 op 最终是读、写还是别的 descriptor 动作

### 54.2 `CUserComm` 的跨模块装载点当前更像在 `libViewthem.so`，而不是 `libtaapiw.so/libtpdata.so`

- 重新核对 ELF 依赖后，当前可以先把一个误差修正掉：
  - `libtaapiw.so` 没有直接 `NEEDED libTdxAsioComm.so`
  - `libtpdata.so` 也没有直接 `NEEDED libTdxAsioComm.so`
- 相反，`libViewthem.so` 的 ELF 依赖里明确存在：
  - `NEEDED libTdxAsioComm.so`
  - 同时动态导入 `MakeUserCommModule`
  - 同时动态导入 `DelUserCommModule`
- `tdxw` 自己也同时：
  - `NEEDED libViewthem.so`
  - `NEEDED libTdxAsioComm.so`
  - 并导入 `MakeUserCommModule/DelUserCommModule`
  - 另外还导入 `dlopen/dlsym`

- 在 `libViewthem.so` 里，当前已经直接抓到至少两处创建和三处释放调用点：
  - `0x42848 -> MakeUserCommModule@plt`
  - `0x42c0f -> MakeUserCommModule@plt`
  - `0x41fc4 / 0x42489 / 0x42d18 -> DelUserCommModule@plt`
- 这些窗口附近还暴露出一组本地包装对象字段：
  - `*(obj+0x0)` 保存 `MakeUserCommModule` 返回的 `CUserComm*`
  - `obj+0x10` 附近被当作原子/同步标志使用
  - `obj+0x30/+0x38` 附近则和 `WaitForSingleObject/CloseHandle/TerminateThread` 一类本地线程句柄逻辑绑在一起
- 因而从当前静态证据看，更合理的模块分工应当修正成：
  - `libViewthem.so` 不只是早先误判过的“旧组包层”
  - 它至少仍然是 `CUserComm` 的直接拥有者/装载者之一
  - `libtaapiw.so/libtpdata.so` 更像在更高层走 session/job/appcore 管理，而不是直接链接 `libTdxAsioComm.so`

- 这里需要特别注意一个边界：
  - 发现 `libViewthem.so` 直接创建 `CUserComm`，并不自动等于“最终 7709 行情包就在 libViewthem 里组出来”
  - 它只说明跨模块交接点比之前收敛得更靠上层，至少有一条 owner/loader 链明确落在 `libViewthem.so`
  - 后续更合理的方向是：从这些 `0x42848 / 0x42c0f` 调用点继续追，看看 `libViewthem.so` 是只负责创建/销毁通信对象，还是也直接消费其 vtable 槽位

- 继续把 `MakeUserCommModule` 返回后的窗口拉开后，当前已经能看到 `libViewthem.so` 不只是 owner/loader：
  - 在 `0x423d6`，它对 `*(userComm->vtable + 0x30)` 做了直接虚调用
  - 在 `0x42b81/0x42ba0` 这一路，它先取 `*(userComm->vtable + 0x40)`，再做直接虚调用
  - 在 `0x42423`，它又对 `*(userComm->vtable + 0x68)` 做了直接虚调用
- 若以当前已经固定的 `CUserComm` vptr 基址 `0x34e510` 来解释，这三个位移最接近：
  - `+0x30` -> `CUserComm_StartWorkers`
  - `+0x40` -> `CUserComm_ResolveAndConnect`
  - `+0x68` -> `0x223c0` 这一条更靠后的单次读接口
- 继续把 `CUserComm` vtable 往后扩一段后，尾部顺序已经可以更准确地写成：
  - `+0x50` -> `CUserComm_SendRaw (0x121f10)`
  - `+0x58` -> `CUserComm_Read (0x1222b0)`，会循环读取直到满足请求长度
  - `+0x60` -> `0x1255d0`，形态上更像“带 timeout 的整段读取”包装
  - `+0x68` -> `0x1223c0`，形态上更像“单次读取 / read some”接口
  - `+0x70` -> `0x124730`，仍是更重的异步/带 timeout 读路径候选
- 其中 `0x1223c0` 已能和 `libViewthem.so:0x42423` 的现场直接对上：
  - 调用前是 `rdi=userComm, rsi=buffer, edx=0xee5c`
  - `0x1223c0` 本体一进来就是 `this / buf / len` 形态，并直接落到 socket `error_wrapper`
  - 所以这里更像同步单次读取，而不是此前误写的 `ReadSomeWithTimeout`
- 更关键的是，继续横向扫 `libViewthem.so` 的虚调用偏移后，已经抓到它直接调用 `CUserComm` 的同步收发槽位：
  - `0x420d0` 和 `0x42930` 都是 `call *(vptr + 0x50)`，对应 `CUserComm_SendRaw`
  - `0x429b0` 是 `call *(vptr + 0x58)`，对应 `CUserComm_Read`
  - `0x42a30` 是 `call *(vptr + 0x68)`，对应前面刚纠正出来的 `0x1223c0` 单次读槽位
- 其中 `0x42930 / 0x429b0` 这对窗口的形态很规整：
  - 先对 `this+0x108` 做 `WaitForSingleObject(..., 0x1388)`
  - 成功后把 `rdi=userComm, rsi=buffer, rdx=len` 喂给 `+0x50` 或 `+0x58`
  - 结束后再 `ReleaseMutex`
- `0x42a30` 也复用了完全相同的 mutex 包装，因此这一段已经可以稳定理解为 `libViewthem.so` 对 `CUserComm` 的一组同步 `send / read-exact / read-some` 包装器。
- 这说明 `libViewthem.so` 已经不只是“通过 CUserComm 建连和读状态”，而是明确直接拿它做同步发送与同步整段读取。
- 再往这些同步包装器的 caller 里看，已经能抓到一条很像初始化握手的小包序列：
  - `0xace00`：构造 4 字节常量 `0x02000205`，再经 `0x428f0` 发送 4 字节
  - `0xace48..0xacee7`：构造一个 9 字节结构，开头是 `WORD 0x0104`，后面拼入 `port` 和 `inet_addr(host)`，再经 `0x428f0` 发送 9 字节
  - `0xacef5..0xacf33`：随后清空缓冲，经 `0x42970` 读取 2 字节，并检查返回是否为 `00 5a`
  - `0xacd94..0xacdac`：另一条路径会在发送可变长文本后，经 `0x42970` 读取 `0x24` 字节固定响应，并据此进入后续分支
- 此外，`0xad270` 之后还能看到 `0x429f0` 被拿来读 `0x1f4` 字节，这表明 `+0x68` 这条单次读包装也已经进入同一条握手/初始化序列，而不是孤立的状态查询口。
- 继续把 `0xad830` 这一组 builder 的 format 字符串解出来后，这条“可变长文本发送”链已经可以定性：
  - 使用的格式串是 `CONNECT %s:%d HTTP/1.0`
  - 分支字符串里明确出现 `PROXYTO `、`connection established`
  - 相邻字符串还包括 `Proxy-Authorization:Basic `、`WWW-Authenticate: Negotiate`、`WWW-Authenticate: NTLM`
- 对应代码行为也能对上 HTTP 代理 CONNECT：
  - `0xad830..0xad8f6` 先把 `host/port` 格式化成 CONNECT 请求并经 `0x428f0` 发送
  - `0xad970..0xad988` 再经 `0x42970` 读取固定 `0x27` 字节响应头窗口
  - `0xad9c0..0xad9e7` 会把响应转小写后搜索 `connection established`，命中后再经 `0x429f0` 读取后续 `0x1f4` 字节
- 因此这一整条 `0xad830` 附近的可变长发送链，更稳的结论应当是“HTTP 代理隧道建立握手”，而不是 7709 行情正文协议本体。它对整体链路仍然重要，因为它解释了某些连接前置握手，但不应再把它与真正的行情包编码点混在一起。
- 再按“同步包装器 vs. 排队发送面”做一层分流后，当前可以更明确地收敛：
  - `0x428f0 / 0x42970 / 0x429f0` 这组三个同步 `send / read / read-some` 包装器，其 caller 当前全部落在同一个 `0xacd..0xad9` 代理握手大函数里
  - 也就是说，已经确认的同步收发面目前只覆盖代理 CONNECT/前置小包握手，还没有直接命中 7709 正文发送
- 与之相对，非代理路径现在开始集中到 `0x42040 / 0x42060 -> 0x42180 -> vtable+0x20` 这条发送面：
  - `0x42060` 会从对象字段里取现成 buffer 指针后跳到 `0x42070`
  - `0x42070` 在可直接发送时会走 `CUserComm_SendRaw(+0x50)`；否则会分配一块新内存、复制 payload，然后把一个 `0x18` 字节描述块交给 `0x42f00`
  - `0x42f00` 已可明确看成发送队列入链 helper：它把 `next / dataPtr / len` 这一类描述块挂到对象 `+0x40` 维护的链表上
  - `0x42180` 则不是最终 socket 层，而是先取上层通信对象虚表 `+0x20` 做进一步提交/调度
- 这条排队发送面的 caller 已经能抓到一批非代理调用点：
  - `0xadb7c / 0xadc08 / 0xadccd` 调 `0x42060`
  - `0xaddcb / 0xae1e7 / 0xae283` 调 `0x42040`
- 继续下钻 `0x42270` 后，当前更像一个围绕 `+0x48/+0x68/+0x88` 这几组队列/锁字段运转的 worker：
  - 先清理和释放积压的发送节点及其 buffer
  - 再回到 `MakeUserCommModule -> StartWorkers / ResolveAndConnect / 读路径` 那套主循环
  - 因此相较于已被判定为代理 CONNECT 的同步发送链，这条“排队发送 + worker 驱动”的路径现在更值得继续追，因为它更像真正的长期业务发送面
- 继续把这条非代理发送面的 producer 拆开后，`0xadb20..0xadc6d` 已经露出更具体的记录格式：
  - 当全局计数 `0x4ecf54` 未超过阈值 `0x1d` 时，它会先在栈上构造一条 `0x0a + payloadLen` 的记录，再直接经 `0x42060` 送入对象 `+0x20` 这条排队发送面
  - 这条记录的头部目前可稳定写成：
    - `byte[0] = 0x01`
    - `word[1] = r15w`
    - `word[3] = r14w`
    - `byte[5] = 0x00`
    - `word[6] = payloadLen`
    - `word[8] = payloadLen`
    - `byte[0x0a..] = payload`
  - 当全局队列 `0x572844` 未满时，同样的记录也会被写入 `0x572860 + index * 0x4e2a` 这片全局缓冲区，然后再通过 `0x42060` 发送或延后排队
- 继续往上追 `0xadb20` 的上一层后，又可以再收窄一层：
  - `0xadb20` 不是一个被多处分散调用的小函数，而是 `0xada60` 这个通用 producer 内部的一条分支；真正的分支点在 `0xadae2`
  - `0xada60` 的实参形态已经比较清楚：`rdi=发送上下文对象`、`rsi=payload 指针`、`edx=payloadLen`
  - 其中头部里的两个 `word` 并不是这条分支内部写死的常量，而是在进入分支前由 `0xadab5/0xadab9` 从全局 `0x4ecf5a / 0x4ecf58` 读到 `r14w / r15w`
  - 这两个全局字在更高层会被不同业务路径反复改写后再调用 `0xada60`；当前已经能看到多组不同赋值，例如：`0x45acb/0x45ae8 -> [4ecf58]=obj+0x80c, [4ecf5a]=0x30`，`0x4b64d/0x4b65c -> [4ecf58]=0, [4ecf5a]=0x55`，`0x4bcdc/0x4bce8 -> [4ecf58]=1, [4ecf5a]=0x72`
  - 因此 `0xadb20..0xadc6d` 更像“统一发送记录封装器”的正文分支，而 `word[1]/word[3]` 更像由上层业务先写入的命令/子命令对，而不是固定协议魔数
- 这也解释了为什么 `0xada60` 的 caller 会很多，而且 payload 长度分布非常散：当前已经能静态抓到 `0xa2f8e / 0xa32c9 / 0xa37a2 / 0xa4bb3 / 0xa4d4d / 0xa51a8 / 0xa54df / 0xa5533 / 0xa5606 / 0xa64c5` 等多处调用点，它们只是复用同一个记录封装器，而不是各自拥有独立的 socket 发送面。
- 到这里可以把“旧协议同步 / 新协议异步”的边界说得更明确：
  - 仓库 legacy Go 客户端在 [client.go](client.go#L198) 的 `SendFrame` 里采用的是典型同步模型：`Write(frame)` 之后立刻 `Wait.Wait(msgID)`，依赖 `msgID -> 单次响应` 配对返回；这和当前仓库中 `GetCount/GetCode/GetGbbq/...` 这一整组 API 的用法完全一致
  - 已确认的 `libViewthem.so` 同步收发包装器 `0x428f0 / 0x42970 / 0x429f0` 目前又只命中代理 CONNECT 和前置小包握手，不是 7709 正文主链
  - 真正的 7709 新主链则落在 `0x42040 / 0x42060 -> 0x42f00 -> 0x42270` 的排队发送面、`CUserComm_StartWorkers`、`boost::asio` 异步 op 包装（`0x124e70 / 0x125290`）以及 `CTDXSession 0x17f9b0` 这类状态机分发器上
  - 再结合 `stream 42` 的长连接分期（资源同步 -> `0x0010` -> `0x000f` -> 资源收尾）与 [docs/7709-protocol-analysis.md](docs/7709-protocol-analysis.md) 里已有的 `Recv 111/112 PushData` 推送特征，当前更稳的总判断应是：旧协议主模型是“同步请求/同步应答”，新协议主模型是“长连接 + 排队发送 + 状态机驱动 + 服务端异步推送”的异步体系
- 在这些 caller 里，已经能先整理出几类有代表性的样本：
  - `0xa54df` 是最直接的一类：直接把 `0x200` 字节 payload 经 `0xada60` 发出，说明这条通用封装器不只处理小控制包，也处理较大的固定长度业务块
  - `0xa5533 / 0xa5583 / 0xa5606` 则是 2 字节小包样本：payload 本身分别写入 `0x1f7e / 0x1f4d / 0x1f7d` 这类本地 tag，再统一交给 `0xada60`，说明“外层记录头里的命令对”和“payload 内部自带的短 tag”是两层不同语义，不应混看
  - `0xa5200` 是一个更有信息量的 wrapper：它先在调用方 buffer 中写入 `WORD 0x1f4c`，随后写两个 `DWORD` 字段，再拼接一段字符串，最后以 `0x0b + strlen` 的长度交给 `0xada60`；而 `0x67dc2..0x67dfd` 这条上游会先把全局命令对写成 `[4ecf58]=0, [4ecf5a]=0x71`，再调用 `0xa5200(..., string, 0, 0x7530)`
  - 换句话说，`0x71/0x0000` 这组外层命令对并不是 payload 本身的 `0x1f4c`，后者是 wrapper 私有的内部 tag；这再次支持“外层命令对 = 业务路由键，payload 内部字段 = 具体结构体内容”这层分工
- 再把 `0x67xxx` 与 `0x4b0xx` 这两簇上层 caller 对比后，当前还能继续把命令对按行为分成两组：
  - `0x71 / 0x73 / 0x75` 这组更偏字符串或路径封装：
    - `0x67dc2..0x67dfd` 把 `[4ecf58]=0, [4ecf5a]=0x71`，随后调 `0xa5200(..., CString, 0, 0x7530)`
    - `0x679a1..0x679dc` 则在另一条分支里把 `[4ecf58]=0, [4ecf5a]=0x75`，随后仍调 `0xa5200(..., CString, 0, 0x7530)`
    - `0x67a76..0x67b2b` 把 `[4ecf58]=0, [4ecf5a]=0x73`，随后调 `0xa5430(leftPart, rightPart)`；而 `0xa5430` 会组出一个固定 `0x200` 字节 payload，结构是 `WORD 0x1f80 + 0xfe 字节字符串 + 0xfe 字节字符串`
  - `0x74 / 0x76` 这组当前更像 UI/控制流里的字符串通知：
    - `0x4b23d..0x4b261` 把 `[4ecf5a]=0x74`，随后调 `0xa5200(..., CString, 0, 0xc350)`
    - `0x4b060..0x4b083` 把 `[4ecf5a]=0x76`，随后也调 `0xa5200(..., CString, 0, 0xc350)`
    - 同一函数周围充满 `GetDlgItem/EnableWindow/SetWindowTextA/ShowWindow`，因此这组目前不应优先视作 7709 正文主链
- 另一个重要纠偏是：`0xa52a0` 不是新的网络 wrapper，而是本地文件 helper。它会对 `0x4ecf60` 这块全局区做长度裁剪，随后 `CFile::Open -> Seek -> Write -> SetLength -> Close`。因此 `0x67c5c` 这条路径更像“处理返回内容后回写本地文件/缓存”，不是新的发送面。
- 继续下钻剩余的 `0x53 / 0x55 / 0x72` 三组后，当前优先级也可以再分层：
  - `0x55 / 0x72` 目前仍偏小控制/通知面：
    - `0x4b644..0x4b669` 把 `[4ecf58]=0, [4ecf5a]=0x55` 后调用 `0xa5560`
    - `0x4bccc..0x4bceb` 把 `[4ecf58]=1, [4ecf5a]=0x72` 后继续走同一簇
    - 这组周围仍然混着 `PostMessageA`、窗口状态判断和 UI 控件流程，当前不像最该优先追的正文主链
  - `0x53` 则值得上调优先级：
    - `0x470a2..0x47171` 把 `[4ecf58]=0, [4ecf5a]=0x53`
    - 随后不是简单拼 CString，而是多次调用 `0xb6500` 取索引/状态，再从 `0x503640 + idx * 0x9a` 这张表里抽取多个字段
    - 最终把这些字段送进 `0xa5140`
    - `0xa5140` 会组出一个固定 `0x18` 字节的二进制 payload：`WORD 0x1f47 + DWORD + WORD + DWORD + BYTE + 可选 7 字节字符串 + DWORD`，然后直接经 `0xada60` 发出
    - 因而这条 `0x53` 链已经不是单纯 UI/文本包装，更像“表驱动参数 -> 二进制请求结构 -> 通用发送封装器”的业务请求链
- 需要注意的是，`0xb6500` 本身不是网络发送：它只是对 `obj+0x40` 窗口句柄发一条 `SendMessageA(..., 0x188, 0, 0)` 取值 helper。真正值得追的是它返回的索引如何驱动 `0x503640` 这张表，以及 `0xa5140` 组出的 `0x1f47` payload 在协议上对应什么业务语义。
- 继续反打 `0x503640` 的上游后，这张表现在可以再细化一层：
  - `0xa4290` 是一条“全局源 -> 工作表 `0x503640`”的预处理路径，最多处理 `0xc8` 条；其中 `0x572558` 这个字节会决定它是直接拷贝现成的 `0x9a` 条目，还是把另一种原始 `0x90` 记录规范化后再写入 `0x503640`
  - 在“直接拷贝”分支里，代码会逐条把 `entry+0x3f` 先补 `0`，再对 `entry+0x2d` 调 `AllTrim`，说明 `0x2d` 一带是一段需要裁剪的文本键
  - 在“规范化”分支里，原始记录头的两个 `WORD` 会被拆成新条目的 `entry+0x00` 与 `entry+0x92`；原始记录里的时间戳会被拆成 `entry+0x25=yyyymmdd` 与 `entry+0x29=hhmmss`；同时还会把两段字符串分别抄到 `entry+0x04` 与 `entry+0x2d`
  - `0x76580` 这条 `qsort` 比较器当前也已经能读清：它按 `entry+0x25` 再按 `entry+0x29` 做倒序比较，因此 `0x503640` 最终会按“日期优先、时间次之、越新越靠前”排序
  - `0xa82e0` 则是另一条“来宾缓冲区 -> 工作表 `0x503640` -> 分类缓存池”的同构路径：它在完成同样的规范化和排序后，会把最多 `0x32` 条结果复制到 `0x4d1e80 + (slot * 0x1e14)`，并把条数写到 `0x5296e0[slot]`；其中 `0x1e14 = 0x32 * 0x9a`
- 对 `0x53 -> 0xa5140` 这条消费链，字段映射现在也更明确了：
  - `0x470e3..0x47171` 会先按 `idx * 0x9a` 取条目，再把 `entry+0x25` 读到 `r12d`
  - 随后把 `entry+0x04` 作为十进制字符串送进 `strtol(..., 10)`，把结果放到 `r14d`
  - 再把 `entry+0x02` 作为 `WORD` 读到 `r13d`，把 `entry+0x00` 作为 `WORD` 读到 `esi`
  - 最终传给 `0xa5140` 的参数组合是：`esi=entry[0x00]`、`edx=entry[0x02]`、`ecx=strtol(entry+0x04,10)`、栈上尾参数=`entry[0x25]`，而 `r8/r9` 都固定为 `0`
  - 也就是说，`0x1f47` payload 当前稳定可写成：`WORD 0x1f47 + DWORD(entry[0x00]) + WORD(entry[0x02]) + DWORD(parse10(entry+0x04)) + BYTE 0 + no-string + DWORD(entry[0x25])`
  - 反过来看，`entry+0x29` 和 `entry+0x2d` 这两个在构表/排序中很显眼的字段，当前并没有被 `0x53` 直接消费；这说明 `0x503640` 更像上层共享业务表，而 `0x53` 只是从中抽取一个更小的请求子集
- `0x70388..0x70461` 现在可以看成一条明确的“刷新工作表 -> 视图缓存”业务路径：
  - 调用 `0xa4290` 前，`rdi` 取自 `0x4e6888` 指向的全局对象，`esi` 则由 `bx` 派生：`bx==0x2f` 时传 `0`，其余非 `0x2c` 情况传 `1`
  - 调完 `0xa4290` 后，若 `bx==0xb4`，代码会把当前 `0x503640` 的条数和整块内容复制到某个长寿命对象的 `+0x7850/+0x7854` 区域，大小正好是 `0x7850 = 0xc8 * 0x9a`
  - 这说明 `0x503640` 不只是瞬时工作缓冲区，也会被某些上层视图或对象快照化缓存
- `0xa6e90` 这条围绕 `0x5296e0/0x518820/0x529720/0x529760` 的路径，现在也能拆成两种模式：
  - 入口第二个参数 `esi` 是 slot 索引；`0x5296e0[slot]` 记录该 slot 当前条数，始终会被截断到 `0x32`
  - 当 `0x572558 != 1` 时，函数直接以 `0x4ecf60` 开头的数据为输入，把最多 `0x32` 条 `0x9a` 记录放入 `0x518820 + slot * 0x1e14`，再按 `0x76580` 重新排序
  - 随后它会遍历这个 slot 的所有条目，对 `entry+0x04` 做 `strtol(...,10)`，找出数值最大的那一条；若该最大值超过 `0x529720[slot]`，就把新值写回 `0x529720[slot]`
  - 当 `0x572558 == 1` 时，函数改走与 `0xa82e0` 同构的“原始 `0x90` 记录 -> 规范化 `0x9a` 条目”分支，再放入同一片 `0x518820 + slot * 0x1e14` 缓存区
  - 这一分支不会比较 `entry+0x04` 的数值最大值，而是扫描排序后的条目，找出日期/时间二元组 `entry+0x25 / entry+0x29` 最新的一条；若它比 `0x529760[slot*2 : slot*2+1]` 更新，就把这对 `(date,time)` 写回 `0x529760`
  - 因而当前更稳的理解是：`0x529720` 是按 slot 维护的“最大数值水位”，`0x529760` 是按 slot 维护的“最新日期/时间水位”
- `0xa6e90` 的直接 caller 现在也已经抓到：
  - `0x6fc21..0x6fc34` 会先从全局 `0x4fbdc0` 读出两个 `WORD`：`WORD [0x4fbdc0+0x5] -> r13w`，`WORD [0x4fbdc0+0x7] -> bx -> r12d`
  - 随后 `0x6fc31..0x6fc53` 用 `r12d-0x29` 作为 jump-table 索引跳入一大片 case 分发，因此这层更像“全局状态里的命令/字符码驱动器”，而不是直接从窗口消息取键盘值
  - 在其中一条 case 上，`0x6ffa0..0x6ffdf` 会把 `r12d` 减去 `0x61`，然后把结果作为 `esi` 传给 `0xa6e90`
  - 调用成功后，它还会继续调用 `0x72800`，随后对窗口句柄发一条带 `rdx=slot` 的 `SendMessageA`
  - 同一簇里还有一条平行路径：`0x6ffe8..0x70027` 把 `r12d` 减去 `0x5a` 后传给 `0xa66e0`
  - 这说明 slot 很可能不是随意整数，而是由 `0x4fbdc0+0x7` 里那枚全局 `WORD` 经减基址得到的分类索引；但在继续找到 `0x4fbdc0` 的写入点之前，还不能把它草率命名成“键盘字母”或“首字母过滤器”
- `0xa66e0` 这条姐妹路径现在也能和 `0xa6e90` 更稳地并列起来：
  - 它不复用 `0xa6e90` 的缓存区，而是维护另一套独立的全局：`0x5187a0` 记条数、`0x512d60 + slot*0x120c` 存记录、`0x5187c0` 记最大数值水位、`0x5187e0` 记最新日期/时间水位
  - 它的容量上限是 `0x1e` 条，而不是 `0x32`；单个 slot 的缓存跨度是 `0x120c = 0x1e * 0x9a`
  - 在 `0x572558 != 1` 时，它和 `0xa6e90` 一样会扫描 `entry+0x04` 的十进制值并更新数值水位；在 `0x572558 == 1` 时，同样会走“原始 `0x90` 记录 -> `0x9a` 规范化条目”的分支，并改比较 `entry+0x25 / 0x29`
  - 因而当前更稳的说法是：`0xa66e0` 与 `0xa6e90` 是两套并列的 slot cache manager，结构同构、容量不同、减基址不同
- 再往前追一步后，`0x4fbdc0` 这块全局状态的角色也更清楚了：
  - `0x127f44` 在 `INFO_Init` 里会把 `0x4fbdc0` 起始的 `0x10` 字节整块清零，因此它至少是一个运行时状态头，而不是纯只读配置
  - 更关键的是 `0xae0e7..0xae0f2`：这里会把对象 `rbx+0x158` 开始的 16 字节头通过 `movdqu/movups` 整块拷到 `0x4fbdc0`
  - 这意味着后续 `0x6fc21` 读到的 `WORD[0x4fbdc0+0x5]` 与 `WORD[0x4fbdc0+0x7]`，本质上分别就是该对象头里的 `WORD[rbx+0x15d]` 与 `WORD[rbx+0x15f]`
  - 因而当前更稳的理解不再是“谁给 `0x4fbdc0+0x5/+0x7` 单独赋值”，而是“谁构造了 `rbx+0x158` 这 16 字节对象头”
  - 同一条路径在 `0xae168` 还会把 `WORD[0x4fbdc0+0x7]` 送进 `0x6eb00`；而 `0x6eb00` 的实际逻辑很薄，只是返回 `di > 0x50` 的布尔值。因此这里至少能说明：`+0x7` 这枚字段会被当作一个可与 `0x50` 比较的类别/阈值码，而不是普通长度字段
  - 继续把 `rbx+0x158` 的写入点分类后，当前可以把三类来源分开看：`0x11e5ad`、`0x8c614`、`0x60eee` 这类写入更多是在对象初始化时塞入默认指针、容器边界或临时返回值；它们会写到 `+0x158`，但看不出与 `WORD[+0x15d/+0x15f]` 的业务语义直接相关
  - 真正高价值的是 `0xaed30` 与 `0xaee50` 这两个整块拷贝 helper：它们会把 `rsi` 指向的源记录连续复制到目标对象 `rdi+0xe8 .. +0x180`，其中 `0xaed9e/0xaeecd` 正是 `movups [rdi+0x158], xmm0`。按写入跨度算，复制大小正好覆盖一条 `0x9a` 字节记录
  - 这两个 helper 的 caller 已经能说明 source 不是临场拼装，而是调用者自己持有的嵌入式记录：例如 `0x10cdc2/0x10ce9b` 传入的是 `rbp+0x2f0`，`0x113056/0x1130f3` 传入的是 `rbx+0xd0`，`0x7d6a1` 传的是栈上的临时记录，`0xa2b4a` 传的是 `rbx+0xcc`
  - `edx/ecx` 传给 `0xaed30/0xaee50` 的那一位当前更像 UI/刷新模式开关，而不是记录本身的一部分：同一 source 记录可在 `edx=0` 与 `edx=1` 两种情况下分别进入 `0xaed30` 与 `0xaee50`
  - 更关键的是，`0x11313e/0x113153/0x113166/0x11317b` 已经直接从这块嵌入式记录里抽字段参与请求：`WORD [rbx+0xd0]`、`WORD [rbx+0xd2]`、`DWORD [rbx+0x162]`、`WORD [rbx+0x166]` 会在设置 `0x4ecf58/0x4ecf5a` 后一路送入后续调用
  - 这说明当前更接近真实参数来源的，不是 `0x4fbdc0` 本身，而是这些 caller 持有的 `0x9a` 记录对象；`0x4fbdc0` 只是其中某个活动对象在运行时被截出的 `+0x158` 头快照
  - 再往上追后，这批“带 `+0x162/+0x166` 的嵌入式记录”已经能挂到更具体的对象上：`0x112d60` 会构造一个大小 `0xfd0` 的 `CDialog` 派生对象，分配 `+0xc0` 缓冲区，并把 `+0xd0..+0x16a` 整段清零；其中 `0x112dd3` 明确把 `QWORD [rbx+0x162]` 置零，随后一路 `rep stos` 覆盖到 `+0x16a`
  - 这条对话框对象随后在 `0x113580..0x1136a5` 做大量 `DDX_Control/DDX_Check/DDX_CBIndex` 绑定，说明它不是临时协议容器，而是一个完整的 INFO 类业务对话框；`+0xd0` 这块记录是该对话框的内嵌状态，而不是独立全局表项
  - `0x127c40` 已经坐实为这类对象的 lazy singleton factory：它在全局 `0x571c88` 为空时 `new 0x170`，再调用 `0x112d60` 构造对象，并立刻调 `0x113200` 做初始化/展示前逻辑。相邻的 `0x127cc0` 则会在 `0x571ca8` 下创建另一种 sibling dialog，走 `0xa2860 -> 0xa2a40`
  - 上层入口也已经能对上 INFO 命名：附近导出/符号串显示 `INFO_ShowDlg`、`INFO_ShowFunc`、`INFO_AskZxgRealinfo`、`INFO_ReqGGCjzx` 同簇工作；其中 `INFO_AskZxgRealinfo` 会先调 `0x127bc0` 拉起一个同类 dialog，再把命令对写成 `0x00/0x57` 后下沉，而 `INFO_ReqGGCjzx` 会设置 `0x4ecf58/0x4ecf5a` 并继续调 `0xa72f0`
  - 另一个重要上游是 `INFO_GetMineTitles`：`0x129ef8..0x129f03` 会按 `count * 0x9a` 直接 `memcpy` 一批记录，签名中出现的 `ext_info_title` 基本可以视为这批 `0x9a` INFO 记录的外部承载结构。这说明当前追到的请求参数对象，很可能就是 INFO 标题/条目数据在 dialog 内部的对象化副本，而不是与 `0x503640` 完全无关的另一套格式
  - `0x113200` 现在也能更具体命名：它本身只是 `Create(resource=0xfd0, parent)` 的薄 wrapper，但围绕它的逻辑里存在一条高价值特殊分支。当前对象会先通过 `0xa4dd0` 填充 `+0xc0` 文本缓冲，若返回长度满足 `len == 0xc350` 且 `WORD [obj+0xd2] == 0`，并且当前已写入字节数 `DWORD [obj+0xc8]` 仍小于 `len-0xc350`，就进入 `0x113110`
  - `0x113110` 正是目前最接近业务请求的 INFO dialog 分支：它先调 `0x127c40` 确保 `0xfd0` dialog singleton 存在，再把命令对写成 `0x00/0x6f`，随后把 `WORD [obj+0xd0]`、`WORD [obj+0xd2]`、`char* [obj+0xd4]`、`DWORD [obj+0x162]`、`WORD [obj+0x166]`、`DWORD [obj+0xf5]` 一起传给 `0xa4ae0`
  - 因而当前更像是：`0xfd0` INFO dialog 在展示文本列表/标题之外，还承载了一条“从当前条目直接发起 0x6f 类请求”的执行分支；这里的 `+0x162/+0x166` 不只是 UI 附属字段，而是请求入参的一部分
  - 与之相对，`0x1132b0` 不是单纯 memcpy helper：它会先把外部 `0x9a` 记录整块写入 `obj+0xd0..`，再把命令对写成 `0x00/0x6f`，随后以 `QWORD [0x4e6888]` 为上下文再次调用 `0xa4ae0`。因此它本质上是“复制外部 INFO 条目后立即发起同类请求”的第二入口，而不是只做状态同步
  - `0x113110` 与 `0x1132b0` 的差异主要在附加栈参数：前者把 `DWORD [obj+0xc8]` 和常量 `0xc350` 作为 payload 尾部字段传下去，不带记录指针；后者则把 `0`、`0xc350`、`&obj+0xd0`、`DWORD [obj+0x162]` 和一个全局布尔一起压栈，说明 `0xa4ae0` 支持“可选条目指针 + 可选附加模式位”这两类扩展语义
  - 继续展开 `0xa4ae0` 后，现在可以把它稳定看成一层通用 INFO 请求组包 helper：函数先在调用方给的发送缓冲里组一个固定 `0x1e` 字节 payload，头部 `WORD [0x00] = 0x1f44`，随后写入 `WORD [0x02] = arg1`、`WORD [0x04] = arg2`、`DWORD [0x06] = strtol(arg3, 10)`、`BYTE [0x0a]`、`DWORD [0x12] = arg4`、`DWORD [0x16] = stackArg7`、`DWORD [0x1a] = stackArg8`，最后经 `0xada60(ctx, payload, 0x1e)` 下送到统一 producer
  - `BYTE [0x0a]` 这一个字段还有分支：若第二个 `WORD` 参数为零，则直接取 `r9b`；若非零，则会把一个时间/序号样式的数值格式化成 6 字节文本后复制进去。也就是说，`0xa4ae0` 不是简单透传字段，而是会做一层规范化编码
  - `0xa4ae0` 本身不决定外层命令对：`0x113110/0x1132b0` 会先把 `0x4ecf58/0x4ecf5a` 设成 `0x00/0x6f`，而 `0xe0d20`/`0xe0ee0` 这类 caller 会先设成 `0x00/0x88`，`0x45ad8`/`0x45d8b` 这一簇则会先设成动态值/`0x30`。更稳的模型应是“caller 先选 outer command pair，再复用 `0xa4ae0` 生产内部 `0x1f44` payload”
  - `0xa4ae0` 还有一条由全局 `0x572558` 控制的替代分支：当该字节等于 `1` 时，不再发送 `0x1f44/0x1e` 记录，而是改组 `0x48` 字节的 `0x264b` 或 `0x264e` 记录，再同样经 `0xada60` 下送。这说明它更像“资讯类请求的通用 payload builder”，而不是只服务某一个 `0x6f` 子流程
  - 横向抽样 caller 后，这个判断进一步被坐实：`0x12f93f` 落在已命名的 `INFO_Gen_GetCJZXContent` 内，也直接复用 `0xa4ae0`；`0xe0d9c`/`0xe0f39` 则位于另一条带定时器/累计长度控制的 INFO 内容获取路径里，同样先写外层命令对，再下调 `0xa4ae0`
  - sibling dialog `0xa2a40` 则更像 `Create(resource=0xfd1, parent)` 的平行 UI 路径：其初始化逻辑 `0xa2a90..0xa2ca0` 也是先填充 `+0xc0` 文本缓冲，但后续主要依据 `WORD [obj+0xcc]` 在 `0x571c90/0x571c98` 两个全局窗口对象之间选路，再把 `obj+0xcc` 起的记录经 `0xaee50 -> 0xaea60` 投递过去；相比 `0x113110`，这条更偏列表/窗口刷新面
  - 目前因此可以给两种 sibling dialog 一个更稳的分工：`0xfd0` dialog 才是和 `0x6f` 请求字段直接绑定的那条路径；`0xfd1` dialog 更像同一 INFO 体系下的辅助展示/分类列表窗口
  - 对 `panzhong.pcapng` 里的长会话 `tcp.stream==42` 做逐包比对后，又坐实了一点：这条主会话并不是另一套正文协议。它和仓库里 `tdxrpc_new/protocol/1728_stream.txt` 记录到的 `c502/b906` 样本属于同一业务命令族，只是外层 6 字节 wrapper 被替换成了全 `0x00`
  - 更具体地说，长会话里这类包的稳定格式应写成：`byte[0x00..0x05]=0x00 * 6`，`word[0x06]=lenLE`，`word[0x08]=lenLE`，`word[0x0a]=cmdLE`，其后才是 body；其中 `len` 统计的是 `cmd + body`，不包含前 10 字节前缀
  - 例如 `frame 467` 的 `c502` 包是 `000000000000 2a00 2a00 c502 <40字节文件名>`，而 `frame 469` 起反复出现的 `b906` 包则是 `000000000000 3601 3601 b906 <offset32> <0x00007530> <302字节文件名区>`。这和 `1728_stream.txt` 中 `0c0518690001 2a00 2a00 c502 ...`、`0c06186a0001 3601 3601 b906 ...` 的差异只落在前 6 字节
  - 因而当前更稳的解释是：`c502` 表示“按名称请求资源文件”，`b906` 表示“按 offset 分块拉取资源内容”；`infoharbor_block.dat`、`tdxhhy.cfg`、`infoharbor_ex.name`、`infoharbor_ex.code` 等都属于这条资源同步链
  - 这也给当前主线一个重要边界：`stream 42` 前半段虽然确实是 7709 长会话正文，但它主要体现的是资源/配置同步，不是先前重点跟踪的 `0x1f47 / 0x1f4c / 0x1f80 / 0x1f44` 这组业务 payload。因此后续若继续对齐 builder，应把 `c502/b906` 视为另一条“文件/资源获取”子链，而不要和 `0xa5140 / 0xa5200 / 0xa5430 / 0xa4ae0` 混成同一层。
  - 继续把 `stream 42` 往后拆后，当前已经能看到一个清晰的阶段切换：`frame 921` 起不再是 `c502/b906`，而是批量代码列表请求。其公共外层仍是 `000000000000 + lenLE + lenLE + cmdLE`，但命令字已经切到 `0x0010` 与 `0x000f`
  - `0x0010` 阶段的请求体模型已经比较稳定：payload 第一个 `DWORD` 是本批代码数量，随后是以 `0x00` 分隔的一串 6 位代码文本。已确认样本包括：
    - `frame 921`：`count=100`，代码从 `000050/000551/.../159228` 这一批开始
    - `frame 928`：`count=100`，继续 `159229..159378`
    - `frame 1002`：`count=38`，是这一阶段的尾包，内容收束到 `589380..920455`
  - 这些 `0x0010` 请求的服务端回包会直接回同命令字的压缩体，例如 `frame 924` 的响应头是 `b1cb7400 1c00 0000 0000 1000 6b0f de37`，即可解为 `cmd=0x0010`、`zipLen=0x0f6b`、`rawLen=0x37de`
  - 在 `frame 1029/1032/1035`，命令字继续切到 `0x000f`。这一组请求体仍然是“批次数 + NUL 分隔代码串”模型，只是批次数分别变成 `0x57(87)`、`0x43(67)`、`0x1e(30)`；代码内容也明显转向更像行情股票池的集合，例如 `300190/300243/300277/301327/.../920961`
  - `0x000f` 的回包尺寸明显比 `0x0010` 更大，且同样直接回同命令字的 zlib 压缩体：例如 `frame 1030` 的响应头是 `b1cb7400 1c00 0000 0000 0f00 4421 5c66`，可解为 `cmd=0x000f`、`zipLen=0x2144`、`rawLen=0x665c`
  - 一个新的旁证是：仓库旧协议常量里，`0x0010` 被标成 `FINANCE/KMSG_FINANCEINFO`，`0x000f` 被标成 `EXDIVIDEND/KMSG_XDXRINFO`。这不能直接证明当前长会话与 legacy `/protocol` 同构，但至少说明这两个命令字在通达信体系里长期就和“财务信息 / 除权除息信息”绑定
  - 同时也要明确两者不是同一层编码。legacy 单股 `0x000f` 请求在 [protocol/model_gbbq.go](protocol/model_gbbq.go) 里只是很短的 `01 00 + exchange + 6位code`；而 `stream 42` 里的 `0x0010/0x000f` 明显是“count32 + NUL 分隔代码串”的批量容器。因此当前更稳的判断是：长会话阶段大概率在做“财务/复权相关元数据的批量预取”，但编码层已经不是 legacy 单股 frame，而是新客户端自己的批量启动协议
  - 因而当前最稳的阶段划分应改成三段：
    - `stream 42` 前半段：`c502/b906` 资源同步子链
    - 中段：`0x0010` 批量代码列表请求，按 100/100/.../38 这类批次下发
    - 后续：`0x000f` 批量代码列表请求，继续按 87/67/30 这类批次下发，并触发更大的压缩响应
    - 但 `0x000f` 也不是流尾的最终业务命令。继续看到 `frame 1041` 后，客户端会短暂回切到一组很小的资源请求：先发 `c502("zd.zip")`，服务端回一条与前面资源同步同形态的版本/哈希样短响应；随后又发两条 `b906("zd.zip")` 分块请求（`offset=0` 与 `offset=0x7530`）
    - 对应的 `zd.zip` 第二块响应并不是新的应用层命令，而是同一个 `b906` 压缩体被拆成了三段 TCP 负载：`frame 1053 (11520 bytes)`、`frame 1055 (11520 bytes)`、`frame 1057 (6030 bytes)`；之后 `1059/1060` 即进入双向 FIN 关连接
    - 因而更稳的说法应是：`stream 42` 的正文核心阶段仍只有“资源同步 -> 0x0010 -> 0x000f”这三段；`0x000f` 之后没有再切出新的正文命令族，而是回到一次小型 `zd.zip` 资源收尾并结束会话
  - 这说明主长会话在资源同步结束后，确实进入了另一类更接近行情数据面的业务阶段；但它仍然不是先前通过 `0xa5140 / 0xa5200 / 0xa5430 / 0xa4ae0` 确认的 `0x1f47 / 0x1f4c / 0x1f80 / 0x1f44` builder 家族，而更像“按股票代码批量取数”的另一条正文子链
- 对 `0x42180` 这一层再补看后，当前还能进一步细化：
  - `0x42160` 取的是对象 `+0x20` 里的上层提交对象，`0x42170` 取的是对象 `+0x28` 里的另一条提交对象，然后统一跳到 `0x42180`
  - `0x42180` 本身只做一个薄分发：取目标对象虚表 `+0x20`，按 `(obj, 0, len, bufOrDesc)` 形态把请求继续上抛
  - 其中 `0x42170` 当前已经能对上一条更偏控制面的 caller：`0xadf20..0xadf50` 会先清零 `obj+0x168` 起的一大块缓冲区，再以 `WORD [obj+0x164]` 为长度把这块 buffer 经 `obj+0x28 -> vtbl+0x20` 提交；这条更像控制/刷新面，不像正文 producer 本体
- 这说明：
  - `0xadb20..0xadc6d` 已经不是简单的连接/代理控制壳，而是在真正批量生产一类“固定 10 字节头 + 变长 payload”的业务发送记录
  - 相比 `add20` 和 `ae300` 这种只负责计数、阈值、线程创建和清理的控制层，这段 producer 更接近 7709 正文发送前的上游封装层
- 这意味着：
  - `libViewthem.so` 不是只创建 `CUserComm` 然后把它交给别的模块
  - 它至少已经直接消费了 `CUserComm` 的“启动工作线程 / 发起连接 / 同步发送 / 同步读取 / 单次读取”这类核心槽位
  - 因而此前把 `libViewthem.so` 整体降级成“完全不相关的旧链”是不准确的；更稳的说法应当是：它未必是最终 7709 行情包的唯一组包层，但它确实直接参与了 `CUserComm` 的通信对象生命周期和接口消费

### 46. `Connect / Exit / AbortLoginOperator Exit / CommitLoginSuccess` 这组壳层函数已经闭环

- 继续按日志字符串对齐后，现在可以把这组 `CTDXSession` 壳层处理器稳定命名为：
  - `0x17bee0` -> `CTDXSession Connect Session=%p,Client=%p`
  - `0x17bfd0` -> `CTDXSession ExitStart Exit Session=%p,Client=%p`
  - `0x17c0d0` -> `CTDXSession ExitComplete Exit Session=%p,Client=%p`
  - `0x17c1d0` -> `CTDXSession AbortLoginOperator Exit Session=%p,Client=%p`
  - `0x17c2f0` -> `CTDXSession CommitLoginSuccess Session=%p,Client=%p`
- 这几条函数的共同特征是：
  - 日志打印后，大多只做一次 `0x177730(this, eventCode, 0)`
  - 再对 `this + 0x550` 这段同步对象做锁/清理
  - 条件性设置 `this + 0x4e0`
- 因而它们基本都属于 session 状态机壳层，而不是协议编码层。

### 47. 退出簇和登录成功簇内部事件码已经可恢复

- 从这组壳层函数里，当前已经能读出几组固定事件码：
  - `Connect` -> 传给 `0x177730` 的事件码是 `2`
  - `ExitStart` -> 事件码是 `8`
  - `ExitComplete` -> 事件码是 `9`
  - `AbortLoginOperator Exit` -> 事件码是 `5`
  - `CommitLoginSuccess` -> 这里的早期记录曾暂记为 `5`，但后续按 `0x27c2f0` 指令级复核后已修正为 `4`
- 因而当前更稳的说法是：
  - `AbortLoginOperator Exit` 走 `event 5`
  - `CommitLoginSuccess` 走 `event 4`，并在 `this + 0x4f0 != 0` 时额外补一跳 `0x177e80(this, 5)`
- 两者虽然都会触发后续状态机动作，但其含义并不相同：
  - `AbortLoginOperator Exit` 更像失败/中止后的退出处理
  - `CommitLoginSuccess` 后续会落到另一条更重的逻辑路径，并与 `0x17c440 / 0x17f710` 发生关联

### 48. `0x177a40` 的职责进一步明确为“按状态扫描内部表并驱动条目对象”

- `0x177a40` 并不是一个简单的转发器，而是在：
  - 先读取 `this + 0x10` 的条目数量
  - 遍历 `this + 0x8` 指向的一张内部表
  - 找到满足：
    - 条目 `+0x10 == 2`
    - 条目 `+0x4 == state`
    的表项
  - 然后围绕 `this + 0x28` 指向的一组链表/容器做扫描、摘取、重挂和回调
- 在条目对象层，当前已经能看到两类调用：
  - `177730(this, entry->type, entry->payload)` 这种回调/投递
  - 条目对象虚表 `+0x10` 这类下沉调用
- 这说明 `0x177a40` 更像：
  - 按 session 当前状态，从内部运行队列中挑选符合条件的 job/条目
  - 再把它们送入下一层处理器
- 因而它依旧是非常关键的调度器，但仍然不是最终 socket 组包层。

### 49. `0x17c440` 与 `0x17f710` 当前更像“重逻辑处理器”，不是壳层日志函数

- 这两个函数都被 `0x17cc50` 注册进 `CTDXSession` 分发表，而且还会在后续调度函数 `0x17f9b0` 中被按函数指针身份分流。
- 当前已经能确认：
  - `0x17c440`
    - 会读取 `this + 0x500 / +0x508 / +0x510` 这组连接/容器字段
    - 会构造一块本地描述结构，然后把 `+0x508` 里的对象集合复制到临时缓冲区
    - 随后按事件码 `4/5` 等分支，对集合中的对象依次调用虚表 `+0x10`
    - 这更像一个“遍历当前连接/监听对象集合并逐个通知”的重逻辑处理器
  - `0x17f710`
    - 会处理一个特殊事件 `0x11`
    - 在一条分支里取出 job 对象并直接调用 job 虚表 `+0x30` 和首槽
    - 在另一条分支里经 session 虚表 `+0x100` 再次投递事件 `0x11`
    - 这更像一条“登录成功后或某个关键阶段的运行时 job/通知处理器”
- 因而到当前为止，最值得继续追的未命名重逻辑点已经收敛为：
  - `0x17c440`
  - `0x17f710`
  - `0x17ebc0`

### 49.1 `0x17c440` 与 `0x17f710` 的优先级现在已经可以分开

- 继续补齐字段状态后，可以把两者放到不同优先级：
  - `0x17f710` 当前更像特殊 `event 0x11 + CTAJob_CloseEx` 的 close/close-ex 执行桥。它会：
    - 围绕 `event 0x11` 做回投
    - 经 session 虚表 `+0x78` 取 `CTAJob_CloseEx`
    - 执行 job 虚表 `+0x30` 与首槽
    - 失败时改写 `CheckConnect` 一带
  - 因而 `0x17f710` 更像连接生命周期尾部或异常恢复链的一部分，不再像 `0x0010/0x000f` 这类启动期批量预取的首选入口
- 相比之下，`0x17c440` 的形态更贴近“登录成功后启动一批对象动作”：
  - 它直接围绕 `this + 0x500/+0x508/+0x510` 这一组对象集合运转
  - 会复制对象集合到临时缓冲区，再按事件 `4/5` 分别逐个调用对象虚表 `+0x8/+0x10`
  - 而 `CommitLoginSuccess` 同簇区域又已观察到 `0x17c337 / 0x17c341` 会把 `+0x408/+0x404` 成对置 `1`；与之对应，`DisConnCpl` 会把 `this + 0x404 = 0`
  - 再结合 `+0x404/+0x408` 现已更稳地解释为“当前活跃 client / 次级上下文可用位”，当前最合理的更新是：`CommitLoginSuccess` 很可能先把活跃连接上下文标成可用，再先进入 `0x177730(event=4)`；若 `this + 0x4f0 != 0`，则还会再经 `0x177e80 -> 0x177a40(state=5)` 处理一层状态动作队列。`0x17c440` 仍是最强广播候选，但不能再直接写成 `CommitLoginSuccess` 的唯一下一跳。
- 因而若目标是继续逼近 `stream 42` 中资源同步后的 `0x0010/0x000f` 异步批量链，当前优先级应改成：
  - 第一优先：补齐 `CommitLoginSuccess -> 0x177730 / 0x177e80 / 0x177a40` 这一层状态调度到底如何落到具体处理器
  - 第二优先：继续反推 `0x17c440` 广播对象集合里 `event==5` 的 `vtable +0x10`
  - 降级观察：`0x17f710`、`0x17ebc0`

### 49.2 需要避免把不同类上的 `+0x508` 偏移误当成同一字段

- 现阶段有一个很容易走偏的点：
  - 在 `CTDXSession_BroadcastHandler (0x17c440)` 语境里，文档只证明了它会围绕 `this + 0x500/+0x508/+0x510` 这一组对象集合运转，并把 `+0x508` 里的对象集合复制到临时缓冲后逐个通知
  - 在 `TPData_MsgWnd` 语境里，则已经单独证明其大对象布局里 `+0x508` 初始化为 `IFetchData*` 容器，且该对象还同时持有 `+0x500 = CAppCore*`
- 这两处 `+0x508` 目前不能直接当作“同一个对象字段”处理；更稳的说法只能是：
  - `CTDXSession` 在登录成功后，很可能通过 `0x17c440` 向某个对象集合广播启动事件
  - 而这个集合中的某个成员，后续很可能就是 `TPData_MsgWnd` / fetch handle manager / `CAppCore` 相关执行者，进而触发资源同步与批量预取
- 也就是说，当前最值得验证的假设已经更新为：
  - `0x17c440` 广播到的对象集合里，至少有一个对象会落到 `OnGetSession -> SendGetEvtNode -> CreateFetchDataHandle(Ex)` 这条 fetch/预取链
  - 但在拿到更直接的对象类型证据前，不应把 `CTDXSession.+0x508` 和 `TPData_MsgWnd.+0x508` 直接合并命名

### 49.3 当前宏观启动链已明显收窄，但 `CommitLoginSuccess` 与 `BroadcastHandler` 之间仍隔着一层状态调度

- 继续把 `CTdxEvtztWnd::OnGetSession -> SendGetEvtNode -> CreateFetchDataHandle(Ex)` 这条链回挂后，现在可以形成一个更完整但仍保守的启动图：
  1. `CommitLoginSuccess (0x17c2f0)` 之后，活跃连接上下文 `+0x404/+0x408` 被置为可用
  2. `CommitLoginSuccess` 会先调用 `0x177730(this, 4, 0)`；若 `this + 0x4f0 != 0`，再调用 `0x177e80(this, 5)`
  3. `0x177e80` 不是广播器，而是临时覆盖 `this + 0x20` 后再调用 `0x177a40(this, state)` 的 wrapper；`0x177a40` 则会按 state 扫描 `this + 0x8` 内部表，并围绕 `this + 0x28` 的挂起队列/容器逐项下沉执行
  4. `CTDXSession_BroadcastHandler (0x17c440)` 已确认是 session 总分发表里的广播处理器，会对 `+0x500/+0x508/+0x510` 对象集合按事件 `4/5` 分别调用对象虚表 `+0x8/+0x10`
  5. 在 `tdxframe100.so` 侧，`CTdxEvtztWnd::OnGetSession` 已能稳定对上：
     - 先走 session helper `0x20f5ce`
     - 再 `ITPConn_GetSession(0x65)`
     - 若 session 有效，则给相关窗口批量 `PostMessage(..., 0x41a, ...)`
     - 随后立即级联 `SendGetEvtNode`
  6. `SendGetEvtNode` 再把高层服务请求交给统一分发器 `0x2998e8`
  7. `0x2998e8` 最终创建 `CreateFetchDataHandle / CreateFetchDataHandleEx(..., 0x51f)`，把请求包装成 fetch task 下送
- 因而当前更稳的结论已经不是“还没连起来”，而是：
  - 宏观链路已经收窄到 `CommitLoginSuccess -> 0x177730 / 0x177e80 / 0x177a40 -> TotalDispatcher/BroadcastHandler 候选 -> OnGetSession/SendGetEvtNode -> CreateFetchDataHandle(Ex)`
  - 真正还缺的不是“还有没有中间层”，而是两件更具体的事：
    - `0x177730 / 0x177a40` 最终如何落到 `CTDXSession_TotalDispatcher (0x17f9b0)` 或其他具体处理器
    - `0x17c440` 广播到的对象，其虚表 `+0x10` 具体是哪一个实现，是否直接触发 `PostMessage(..., 0x41a, ...)` 或等价窗口/任务回调
- 这也意味着后续不需要再大范围发散猜测模块关系，主问题已经收缩成：
  - `0x177730 / 0x177a40` 的真实落点
  - `0x17c440` 广播对象的实际类型
  - 以及其 `vtable +0x10` 是否就是把 session ready 事件推进到 `CTdxEvtztWnd::OnGetSession` 的那条桥接实现

### 49.4 `0x17c440` 的 headless 指令级证据已经补齐

- 这次直接用 Ghidra headless 导出了 `CTDXSession_BroadcastHandler (0x27c440)` 的完整指令和交叉引用，当前可以把此前的“宏观推断”收紧成以下硬证据：
  - 该函数开头先对 `this + 0x550` 做一次互斥/保护进入，然后才开始广播流程
  - 广播对象集合确实来自 `this + 0x508`，元素个数来自 `this + 0x510`
  - 函数会先把这组对象指针复制到栈上的临时缓冲，再迭代该快照；因此这里不是边遍历边直接操作原容器，而是明显做了一层“广播快照”
  - 迭代时传给目标对象的第二个参数固定是 `this + 0x58`，也就是 session 内部某个稳定子对象，而不是简单的事件码/整数
  - 两个事件分支现在已经可以直接按指令对上：
    - `0x27c75c: call [rax + 0x8]` 对应 `event == 4`
    - `0x27c6b2: call [rax + 0x10]` 对应 `event == 5`
- 这说明 `0x17c440` 的真实角色不是“自己直接发网络包”，而是“登录/状态切换后，拿 session 上下文对象去唤醒一组订阅者/桥接对象”。
- 同时，它的调用方当前也已经收窄：headless 反查结果里，指向 `0x27c440` 的已命中代码引用来自 `CTDXSession_TotalDispatcher (0x27f9b0)`，说明这条广播链确实挂在 session 总分发表内，而不是任意 UI 零散路径直接调用。
- 因而当前最稳的更新结论是：
  - `CommitLoginSuccess -> TotalDispatcher/状态分发表 -> BroadcastHandler -> 对象虚表 +0x8/+0x10`
  - 其中我们要继续追的主桥仍是 `event==5` 对应的 `vtable +0x10`，因为它最像“session ready 后启动后续取数/窗口回调”的那条异步通知面

### 49.5 `0x177a40` 已确认是 state-action drain，不是直达 `0x17c440` 的单跳桥

- 这次继续用 headless 直接导出 `0x177a40` 后，可以把它的职责收紧为一层“按状态扫描内部表并清空/下沉挂起动作”的调度器：
  - 它先读取 `this + 0x10` 作为内部表项个数，再在 `this + 0x8` 指向的表里筛选 `entry + 0x10 == 2` 且 `entry + 0x4 == state` 的条目
  - 命中条目后，会围绕 `this + 0x28` 中与该条目索引对应的挂起槽位继续处理，而不是直接调用 `0x17c440`
  - 处理过程中会迭代链表/容器节点，经 `0x177f70` 组装本地描述，再把后续工作重新下沉到 `0x177730`
  - 函数尾部会清空已消费的挂起槽、释放相关临时对象，然后继续扫描下一条匹配的 state entry
- 对 `0x177f70` 的继续下钻也进一步排除了它是“最终处理器桥”的可能：
  - 它本质上是在 `0x177a40` 的局部队列对象上维护一个 `0x20` 字节节点池
  - 若空闲链非空，就摘一个节点并把 `RSI` 指向的两段指针写入节点 `+0x10/+0x18`
  - 若空闲链为空，则按 `count * 0x20` 的块缓冲扩容后再分配节点
  - 最后把新节点挂到当前队列尾部
- 因而 `0x177f70` 更像 `state pending queue node prepare` / 小型 free-list 管理器，而不是把状态直接映射到 `0x17f9b0` 或 `0x17c440` 的分发器。
- 因而当前不能再把它记成“`CommitLoginSuccess` 到 `BroadcastHandler` 的最后一跳”。更准确的说法应是：
  - `0x177a40` 负责 drain 某类 state/action 挂起队列
  - `0x17c440` 则是 session 总分发表中的广播处理器
  - 两者都属于 `CTDXSession` 的异步状态机，但目前尚无直接证据证明 `0x177a40` 会单跳调用 `0x17c440`

### 49.6 `+0x508/+0x510` 广播集合的注册/摘除函数已经坐实

- 继续顺着 `+0x508/+0x510` 的真实写点往下读后，现在已经能把这组广播对象集合的维护函数直接钉住：
  - `0x27b940`：注册函数
  - `0x27ba40`：摘除函数
- `0x27b940` 的行为非常明确：
  - 开头先锁 `this + 0x550`
  - 读取 `this + 0x510` 作为当前元素个数
  - 若 `this + 0x508` 指向的指针数组里已存在 `RSI` 这个对象指针，则直接返回
  - 若不存在，则经 `0x281df0(this + 0x500, count + 1, -1)` 扩容，再把 `RSI` 追加写入 `this + 0x508[count]`
- `0x27ba40` 则是对称的摘除函数：
  - 同样先锁 `this + 0x550`
  - 在线性扫描 `this + 0x508` 找到目标对象指针后，若后面仍有元素，则调用 `0x186850` 把尾部元素前移覆盖
  - 最后把 `this + 0x510` 减 1
- 这两段代码共同说明：
  - `CTDXSession` 的广播对象集合不是链表，也不是 map，而就是一块去重维护的对象指针数组
  - `this + 0x500` 更像这块数组/小容器的头部管理区
  - `this + 0x508` 是对象指针数组首地址
  - `this + 0x510` 是当前元素个数
- 还有一个很关键的结构点：
  - `0x27ba30: sub rdi, 0x58; jmp 0x27b940`
  - `0x27bb50: sub rdi, 0x58; jmp 0x27ba40`
  - 这表明注册/摘除函数同时通过 `CTDXSession` 的 `+0x58` 嵌入子接口对外暴露，而不是只有 session 本体内部才能调用
- 因而当前“广播集合写入点”这条待办已经可以收束成一个更具体的结论：
  - 真正的集合写入并不发生在 `0x17c440` / `0x17dbb0` / `0x17b340` 这些广播处理器里
  - 而是发生在 `0x27b940 / 0x27ba40` 这一对专门的 register/unregister helper 里
  - 下一步真正要追的是：谁拿到了 `session + 0x58` 这个子接口，并把哪些对象注册进来

### 49.7 `CTDXSession_EnsureActiveExternalObject (0x27bd10)` 不是广播集合注册点

- 这次顺手把 `CTDXSession_EnsureActiveExternalObject (0x27bd10)` 也重新读了一遍，结论可以进一步收紧：
  - 它只是在 `this + 0x68` 为空时，经 `this + 0x60` 的虚表 `+0x50` 懒取一个“active external object”
  - 成功后把结果写回 `this + 0x68`
  - 若取不到，则会设置 `this + 0x80 = 8` 并写入 `CheckConnect`
- 整个函数并不读写 `this + 0x508/+0x510`，因此它不是广播集合的注册函数，只是另一条“活跃外部对象懒取”路径。

### 49.8 `session + 0x58` 子接口 vtable 已验证，注册/摘除槽位可精确编号

- 继续往构造代码回看后，`CTDXSession` 本体和 `+0x58` 嵌入子接口的 vtable 写入点现在已经能直接对上：
  - `0x178f7c` 一带会把主对象 `this` 的首槽写成 `0x787a70`
  - 同一段在 `0x178fa4` 把 `QWORD [this + 0x58]` 写成 `0x787b88`
  - 另一处 `0x179418..0x17942c` 也再次确认：`[this] = 0x787a70`，`[this + 0x58] = 0x787b88`
- 用 Ghidra 直接 dump 后，`0x787b88`（Ghidra 地址 `0x887b88`）这张表已经能恢复成一张 24 槽的 `CTDXSession` 嵌入接口表，其中关键槽位如下：
  - slot 19 / `+0x98` -> `0x27ba30 -> jmp 0x27b940`：注册广播对象到 `this + 0x508`
  - slot 20 / `+0xa0` -> `0x27bb50 -> jmp 0x27ba40`：从 `this + 0x508` 摘除广播对象
  - slot 21 / `+0xa8` -> `0x27bc00 -> jmp 0x27bb60`：清空广播对象集合
  - slot 22 / `+0xb0` -> `0x27bd00 -> jmp 0x27bc10`：导出 `this + 0x520` 挂起状态三元组
  - slot 23 / `+0xb8` -> `0x27e380 -> jmp 0x27dbb0`：直接桥到 `CTDXSession_RuntimeRecoveryCoordinator`
- 其中 slot 21/22 的本体也已经能读出：
  - `0x27bb60`：锁 `this + 0x550` 后，若 `this + 0x508 != 0` 则按 `this + 0x500` 决定是否释放数组，再把 `this + 0x510 = 0`；它本质上是广播集合 reset/clear
  - `0x27bc10`：锁 `this + 0x550` 后，若 `this + 0x4e0 == 0` 且 `this + 0x530 != 0`，则从 `this + 0x520` 里拷出 `qword[+0x10/+0x18/+0x20]` 到调用者给的输出缓冲，再经 `0x281960` 做一次队列/状态辅助处理；更像“取当前挂起上下文/结果三元组”的 helper
- 这组槽位说明 `session + 0x58` 不是单纯的 getter 视图，而是一张更完整的“订阅对象管理 + 运行时恢复桥 + 状态导出”接口。
- 因而当前更精确的说法应更新为：
  - 广播对象的注册/摘除并不是零散隐藏逻辑，而是 `CTDXSession` 明确通过 `+0x58` 子接口公开出来的高槽位能力
  - 后续在其他模块里要找的，不再是模糊的“也许有个 register helper”，而是更明确的“谁在拿某个 session 接口对象做 `vtable + 0x98/+0xa0` 调用”

### 49.9 `ObserverIfaceDerivedB (0x886c18)` 已可排除为 `event==4/5` 的真正广播观察者

- 继续把 `0x886c18` 这张候选表的 override 本体读开后，关键事实已经比较明确：
  - `BroadcastHandler(0x27c440)` 对 `event==4` 调的是对象虚表 `+0x8`
  - 对 `event==5` 调的是对象虚表 `+0x10`
  - 而 `ObserverIfaceDerivedB(0x886c18)` 在这两个槽位上仍然分别指向默认实现 `0x237d10` / `0x237d20`，两者都是 `xor eax, eax; ret`
- `0x886c18` 真正 override 的是更后面的三个槽位：
  - `+0x20 -> 0x231c00 -> 0x231aa0`
  - `+0x30 -> 0x231a60 -> 0x231920`
  - `+0x38 -> 0x231a90 -> 0x231a70`
- 其中 `0x231920` 的形态已经很像析构/解绑辅助，而不是事件处理本体：
  - 一进来先把对象 vptr 改写成 `0x886b40 / 0x886c18`
  - 随后经对象 `+0x50` 上的 `CTDXSession` 接口取名字（`0x2380c0 = CTDXSessionIface_GetNameThunk`）
  - 再把该名字传给 `this+0x48` 关联对象的虚表 `+0x38`
  - 末尾 `0x231a70` 还会在调用 `0x231920` 后直接跳到公共收尾 `0x6f42b0`
- `0x231aa0` 也更像“匹配当前 session / 状态门控”的辅助槽：
  - 它先比较传入的 session 指针是否等于 `this+0x50`
  - 再通过 `0x238110 = CTDXSessionIface_GetPrimaryContextStateThunk` 检查父 `CTDXSession` 的 `+0x404`
  - 满足条件时仅返回 `1`，不见 `PostMessage`、`CreateFetchDataHandle(Ex)`、`OnGetSession` 这类取数桥的痕迹
- 因而目前应把 `0x886c18` 归类为：
  - 一个带 `this-=0x8` adjustor thunk 的嵌入子接口对象
  - 它覆盖的是对象管理/析构/匹配辅助槽，而不是 `BroadcastHandler` 在 `event 4/5` 时真正使用的回调槽
- 这条纠偏很重要，因为它直接收紧了后续搜索面：
  - 不能再把“任意带 override 的观察者 vtable”都当成广播命中目标
  - 下一批应优先筛选的是 `+0x8` 或 `+0x10` 这两个前部槽位发生 override 的对象，而不是只看后半段槽位有变化的类

### 49.10 两张“前槽 override”假阳性表也已排除：`0x886918` 与 `0x8871f8`

- 按 `.data.rel.ro` 原始字节重新校准后，可以确认并不是所有 `slot1/slot2 != 0x237d10/0x237d20` 的表都属于广播观察者。
- 第一张假阳性表是 `0x886918`：
  - 其前两槽分别是 `0x132e20 / 0x132ea0`
  - 直接看反汇编可见，这两个入口一上来就把对象 vptr 改回 `0x786918`，随后成组清理 `+0x110/+0xd0/+0x78/+0x50/+0x38` 等字段，最后走公共收尾 `0x5f42b0`
  - 同表的下一槽还是 `0x132f30`，而这正是前面已经确认过的 `CAppCore` vtable `+0x20` 方法
  - 因而 `0x886918` 本质上是 `CAppCore` 族接口表，不是 `CTDXSession_BroadcastHandler` 的观察者回调表
- 第二张假阳性表是 `0x8871f8`：
  - 它的前两槽是 `0x1603a0 / 0x1604c0`
  - 这两个入口同样表现为析构/销毁变体：会先把对象多个嵌入子对象的 vptr 改写为 `0x7871f8 / 0x787258 / 0x7872a8`
  - 随后连续销毁 `+0x220` 以内多组子对象，并在尾部重新把宿主对象首槽写成 `0x786d08`，`+0x8` 写成 `0x786d60`
  - 其中 `0x786d60` 正是前面已经识别出的 `ObserverIfaceBaseA`
  - 这说明 `0x8871f8` 更像“持有/聚合一组观察者子对象的 owner/interface”，而不是被广播集合直接存放、由 `event 4/5` 命中的那张回调表
- 目前更稳的边界因此是：
  - `slot1/slot2` 不是默认 no-op 只是第一层筛选条件
  - 还必须继续排除“析构入口 / owner 接口 / CAppCore 族 / 聚合容器表”这类假阳性
  - 真正高价值的候选仍应满足：`BroadcastHandler(event 4/5)` 命中的前槽看起来像业务回调，而不是 vptr 重置和大块对象清理

### 49.11 `0x886b40` 也不是最终目标；真正更像广播观察者的是 `libtpdata.so` 的 fetch handle 双表

- 新补的一步很关键：`0x886b40` 不是一张新的独立观察者表，而是前面 `0x886c18` 那个对象的主表。
  - `0x231920` 一进来会同时把对象首槽写成 `0x886b40`，把 `+0x8` 子对象槽写成 `0x886c18`
  - 这说明此前看到的 `0x886c18` 只是嵌入子接口，而 `0x886b40` 才是宿主对象主表
- 但把 `0x886b40` 的前槽展开后，结论仍然是否定的：
  - 主表前两槽就是 `0x231920 / 0x231a70`
  - 它们仍表现为“vptr 回写 + 名字查询 + 关联对象虚调 + 公共收尾”的析构/解绑辅助
  - 同时它的下一个关键前槽仍然落回默认 no-op，因此并没有出现我们要找的 `event==5` 真实业务回调
- 因而 `0x886b40 + 0x886c18` 这一整组对象现在可以一起降级：
  - 更像 `CTDXSession` 关联对象管理/解绑辅助
  - 不像登录成功后被广播集合直接命中、再继续桥到取数链的 fetch/session 观察者

- 与之相对，`libtpdata.so` 里由 `0x3e924` 构造出来的 fetch handle 双表，开始明显符合“主对象 + 嵌入回调接口”的目标模型：
  - 构造器会把宿主对象首槽写成 `0x2ddd98`
  - 把 `+0x8` 的嵌入子接口槽写成 `0x2dded8`
  - 这与前面在 `libtaapiw.so` 里看到的“主对象 + adjustor 子接口”结构同型，但这里前部回调槽已经不再是默认 no-op

- `0x2dded8` 这张子接口表尤其关键：
  - 第一槽/第二槽仍是析构与 delete 变体：`0x3ea85 -> 0x3e9f6`、`0x3eab4 -> 0x3ea8e`
  - 但第三槽已经不是默认空实现，而是 `0x36dc1`
  - `0x36dc1` 只是一个标准的 `this -= 0x8` adjustor thunk，随后直接跳到 `0x36a5c`
  - `0x36a5c` 明显是业务体：会清理对象 `+0x40` 挂着的缓冲，重置 `+0x1c`，并对传入文本做首字节 `0x24 ('$')` 判定，再继续解析/处理
- 这意味着：
  - 如果 `CTDXSession_BroadcastHandler` 广播集合里存的是嵌入接口指针，那么它命中 `vtable + 0x10` 时，已经能穿过 `0x36dc1` 落到 `0x36a5c` 这个真实处理体

- `0x2ddd98` 这张宿主主表则给出了另一层包装语义：
  - `+0x10 -> 0x36dca`
  - `+0x18 -> 0x36e90`
  - `+0x20 -> 0x36ee8`
  - 这三个入口都不是 no-op，而是按不同模式值向宿主对象更深的虚槽继续分发
  - 例如 `0x36dca` 会先根据对象 `+0xa8` 把模式折成 `2/3`，再调用宿主虚表 `+0x28`，随后再调宿主虚表 `+0x30`
  - `0x36e90` / `0x36ee8` 则分别用固定模式 `1/0` 走同一套“下沉到 `+0x28`，再收尾到 `+0x30`”的封装
- 因而当前更稳的解释已经变成：
  - `libtpdata.so` 的 fetch handle 对象，才是目前最像“登录成功后被 `CTDXSession` 广播命中的真实业务观察者”的候选
  - `0x2dded8` 提供嵌入接口视图，负责把广播回调导入宿主对象
  - `0x2ddd98` 提供宿主主表，负责按模式继续分派到更深的 fetch/session 处理槽

- 这也直接收紧了下一步：
  - 后续优先不再继续盲扫 `libtaapiw.so` 里所有“前槽 override”的表
  - 而是应从 `CreateFetchDataHandle/CreateFetchDataHandleEx` 的注册路径反查：谁把这类 `0x2dded8` 子接口或其宿主对象挂进 `CTDXSession.this+0x508`
  - 一旦把这条注册链补齐，就更有希望把 `CommitLoginSuccess -> BroadcastHandler(event 5)` 闭合到真实的 fetch 启动路径

### 49.12 协议层两层结构已能在 `libtpdata.so` 中对上：外层 `$ZIP$` 压缩壳，内层结构化结果集正文

- 围绕 fetch handle 的真实业务体 `0x36a5c`，现在已经能把“协议两层”落到明确代码节点：
  - 入口参数就是一段 `(buf, len)` 文本/字节块
  - 函数先清空对象 `+0x40` 的旧缓冲并把 `+0x1c` 复位
  - 然后分成两条支路：
    - 普通正文：直接复制到对象 `+0x40`，长度写入 `+0x48`
    - 压缩正文：识别 `$ZIP$` 头后解压，再把解压结果挂到对象 `+0x40/+0x48`

- `$ZIP$` 外层壳已经不是猜测，而是直接命中常量：
  - `0x36b49` 用 `strncmp(..., 5)` 比较的字符串就是 rodata `0xa23d0 = "$ZIP$"`
  - 命中后会继续扫描头部直到 `\r\n`
  - 之后通过 `GetStr(..., '|')` 从该头部中抽出一个字段，再 `atol` 得到解压后的目标长度
  - 最终调用 `tzuncompress` 把后续正文解开并保存到对象 `+0x40`
- 因而目前更准确的外层格式应描述为：
  - 不是单纯“gzip/zlib body”
  - 而是“以 `$ZIP$...\r\n` 作为文本头、后跟压缩正文”的一层传输壳
  - 这层壳负责告诉下游是否需要解压，以及提供至少一个通过 `|` 分隔的长度/元信息字段

- `0x36a5c` 处理完外层壳以后，主表深层槽开始进入真正的正文解释层：
  - `0x2ddd98 + 0x10 -> 0x36dca`
  - `0x36dca` 按对象 `+0xa8` 折出 mode `2/3`
  - 然后转调宿主虚表 `+0x28`
  - 再调用宿主虚表 `+0x30`
  - `0x2ddd98 + 0x18 -> 0x36e90` 与 `+0x20 -> 0x36ee8` 只是把 mode 固定成 `1/0` 后复用同一套下沉路径
- 这说明 fetch handle 主表前几槽不是在做网络 IO，而是在做“正文模式选择器”：
  - 先把收到的正文挂进对象本地缓冲
  - 再按 mode 进入不同的结构化解析策略

- 目前已经能直接恢复出至少两类内层正文结构：

1. 经典结果集模式：`ResultSets -> [i] -> Content / ColDes / ColName`

- 证据链在 `0x3f07e`：
  - 它先从对象 `+0x100` 根结果对象里取 key `ResultSets`
  - 校验当前结果集索引 `+0x13a < +0x138`
  - 取出当前结果集项后，再继续查：
    - `Content`
    - `ColDes`
    - `ColName`
- 这些 key 都能在 rodata 连成一串：
  - `0xa28e2 = ResultSets`
  - `0xa28ed = Content`
  - `0xa28f5 = ColDes`
  - `0xa28fc = ColName`
- 对应到对象字段的语义也已开始清晰：
  - `+0x138`：结果集总数
  - `+0x13a`：当前结果集索引
  - `+0x13c`：当前结果集的内容行数
  - `+0x144`：列数
  - `+0x146`：列定义来源标记
    - `0` 表示来自 `ColDes`
    - `1` 表示来自 `ColName`
- 配套 gate helper 也已经对上：
  - `0x40b22`：检查“当前结果集索引是否还在 `ResultSets` 范围内”
  - `0x40b74`：检查“当前行索引 `+0x140` 是否还在当前结果集行数 `+0x13c` 范围内”

2. mode 3 变体：`hits -> hits -> [i] -> _source`

- `0x3f07e` 在对象 `+0xa8 == 3` 时会切到另一条解析分支 `0x40552`
- 这一分支使用的 key 已经能直接从 rodata 读出：
  - `0xa291d = hits`
  - `0xa2922 = _source`
- 其形态是：
  - 根对象先取 `hits`
  - 再取内部的 `hits` 数组
  - 再用当前结果索引取数组元素
  - 最后取 `_source` 作为当前记录对象
- 因而 mode 3 不是 `ResultSets/Content/ColDes` 这一套，而是另一种搜索/命中列表风格的正文布局
- 这也解释了为什么主表 wrapper 会先按 `+0xa8` 选择 mode：
  - 外层传输壳相同
  - 但内层正文至少存在“经典结果集”与“hits/_source 命中列表”两套结构

- 到这一步，协议层可以更准确地概括为：
  - 第一层：`$ZIP$...\r\n + compressed-body` 或直接 raw-body 的传输壳/封装层
  - 第二层：解压后的结构化正文层，当前已确认至少有：
    - `ResultSets -> Content / ColDes / ColName`
    - `hits -> hits -> _source`
- 这比此前泛泛地说“协议大概是两层”要更实：
  - 现在已经能把两层分别落到 `0x36a5c` 和 `0x3f07e/0x40552` 这些具体实现点上
  - 后续如果再去对抓包或动态日志，优先应看：
    - 外层是否出现 `$ZIP$` 头
    - 解压后正文究竟走的是 `ResultSets` 还是 `hits/_source` 分支

### 50. 当前最有效的后续追踪策略

- 现在不再建议继续从 `Connect/Exit/...` 这些壳层函数硬追，因为它们已经基本证明只是“记日志 + 投递固定事件码”。
- 更有效的方向是：
  - 直接拆 `0x17f9b0` 这类按函数指针分流的调度器
  - 继续下钻 `0x17c440 / 0x17f710 / 0x17ebc0`
  - 重点看它们最终是否把控制权交给：
    - `this + 0x500` 关联对象
    - `job` 虚表 `+0x30`
    - 或某个 `Send/Encode/Fragment` 相关实现
- 只有追到这些“重逻辑处理器”的下游，才更可能真正撞上 `7709` 新行情协议的编码/发包层。

### 51. `0x17f9b0` 已坐实为按“处理器函数指针”分流的总调度器

- `0x17f9b0` 的结构已经非常清楚：
  - 传入参数里直接包含一个“目标处理器函数指针”
  - 它逐个把该指针与已注册的 `CTDXSession` 处理器地址比较
  - 命中后再转调对应的具体处理器
- 当前已经能从 `0x17f9b0` 恢复出的映射包括：
  - `0x17acb0` -> `CreateJob`
  - `0x17ada0` -> `InExecute`
  - `0x17af00` -> `RevcJob`
  - `0x17b020` -> `DisConnCpl`
  - `0x17b180` -> `InNotify`
  - `0x17c440` -> 未命名重逻辑处理器 A
  - `0x17ebc0` -> 未命名重逻辑处理器 B
  - `0x17ef20` -> `ConnectIn`
  - `0x17f710` -> 未命名重逻辑处理器 C
  - `0x17b340`、`0x17b7a0`、`0x17f4f0` -> 仍待补名的同簇处理器
- 这说明：
  - `CTDXSession` 的运行并不是“谁直接 call 谁”的简单链式结构
  - 而是“上层把一个处理器指针交给 `0x17f9b0`，再由它统一分发”
- 对后续逆向的意义很直接：
  - 只要继续补齐 `0x17f9b0` 中尚未命名的处理器身份，就能系统性还原整个会话状态机

### 52. `0x17ebc0` 当前最像“连接失败后的重试/切换处理器”

- `0x17ebc0` 的核心行为现在可以恢复为：
  - 先锁 `this + 0x550`
  - 清零 `this + 0x3f8`
  - 检查 `this + 0x450` 与 `this + 0x452`
  - 若 `+0x450 < +0x452`，则把 `+0x450` 加 1
  - 随后通过对象虚表 `+0x78` 取出一个新对象/新 job
  - 给该对象写入 `this + 0x4dc` 对应的值
  - 再直接调用该对象虚表 `+0x30` 和首槽
- 如果 `+0x450 >= +0x452`，则：
  - 把 `+0x450` 清零
  - 设置 `this + 0x80 = 1`
  - 初始化一组 `0x84` 附近的文本/提示字段
- 从这组行为看，当前最合理的判断是：
  - `+0x450 / +0x452` 很像“当前候选 host 索引 / host 总数”之类的字段
  - `0x17ebc0` 很像连接失败或断连后，尝试切换下一候选目标并重新触发 job 的处理器
  - 也就是说，它更偏“重试/换 host/重建连接任务”的逻辑，而不是最终发包函数本身

### 53. `0x17f710` 与 `0x17ebc0` 的角色分工已出现雏形

- `0x17f710`：
  - 明确围绕特殊事件 `0x11` 工作
  - 一条分支会直接抓 job 对象并调用 job 虚表 `+0x30` 与首槽
  - 另一条分支会把事件 `0x11` 再投递到 session 虚表 `+0x100`
  - 更像“关键阶段事件”的运行时桥接器
- `0x17ebc0`：
  - 明确操作 `+0x450/+0x452/+0x4dc/+0x3f8` 这组连接状态字段
  - 并通过对象虚表 `+0x78` 重新获取要执行的对象
  - 更像连接失败后的重试/切换处理器
- 这说明两者很可能分别位于：
  - 关键事件桥接层
  - 连接失败恢复层
- 它们都还比 socket 组包层更高一层，但已经明显比 `Connect/Exit/...` 壳层更接近真实网络执行链。

### 54. 还未命名但已经进入高优先级的处理器

- 通过 `0x17f9b0`，当前高优先级未命名目标已经进一步收敛为：
  - `0x17b340`
  - `0x17b7a0`
  - `0x17f4f0`
- 其中：
  - `0x17b340` 已知会把 `this + 0x3fc = 0`、`this + 0x4e4 = 1`，再操作 `this + 0x500/+0x508/+0x510` 这组对象集合
  - `0x17b7a0` 已经被 `0x17f9b0` 当作一个独立处理器分支调度
  - `0x17f4f0` 同样被 `0x17f9b0` 单独分流，且与 `ConnectIn/ConnCpl` 这一簇相邻
- 也就是说，后续如果要继续逼近 `7709` 行情协议编码层，最有效的工作面已经更新为：
  - `0x17ebc0`
  - `0x17f710`
  - `0x17b340`
  - `0x17b7a0`
  - `0x17f4f0`

### 55. `0x17b340 / 0x17b7a0 / 0x17f4f0` 现已可以准确命名

- 结合完整函数体与新读出的 rodata，可以把这三条处理器进一步落名为：
  - `0x17b340` -> `CTDXSession InExit`
  - `0x17b7a0` -> `CTDXSession InExitStart`
  - `0x17f4f0` -> `CTDXSession Dormancy`
- 直接对应的日志串已经对上：
  - `0x611e20` -> `CTDXSession InExit Session=%p,Client=%p,Event=%d,State=%d,Job=%p`
  - `0x611e68` -> `CTDXSession InExitStart Session=%p,Client=%p,Event=%d,State=%d,Job=%p`
  - `0x612380` -> `CTDXSession Dormancy Session=%p,Client=%p,Event=%d,State=%d,Job=%p`

### 56. `0x17b7a0 = InExitStart`，负责构造 `exit start` job 并回投事件 `8`

- `0x17b7a0` 的主流程目前可以稳定恢复为：
  1. 通过 session 虚表 `+0x78` 获取一个 job 对象
  2. 对该对象连续写入两项公共错误字段：
     - `0x6055b9` -> `ErrCode`
     - `0x6055c1` -> `ErrType`
  3. 再写入文本值：
     - `0x6126c9` -> `exit start`
  4. 构造本地事件描述块，经 session 虚表 `+0x100` 回投事件
  5. 该事件块首字段固定为 `8`
- 这与前面已经确认的壳层函数 `ExitStart -> fixedEvent 8` 完全一致。
- 因而 `0x17b7a0` 只是“退出开始阶段”的 job 构造与事件回投器，而不是实际的网络编码/发包点。

### 57. `0x17b340 = InExit`，会先广播对象集合，再回投 `exit complete`

- `0x17b340` 在 `InExitStart` 的基础上又多了一层集合广播逻辑：
  1. 锁 `this + 0x550`
  2. 设置退出态字段：
     - `this + 0x3fc = 0`
     - `this + 0x4e4 = 1`
  3. 若 `this + 0x408 != 0`，则把 `this + 0x500/+0x508/+0x510` 这组对象集合复制到临时缓冲区
  4. 遍历集合，对其中对象调用虚表 `+0x18`
  5. 随后再通过 session 虚表 `+0x78` 获取 job，对 job 写入：
     - `ErrCode`
     - `ErrType`
     - `0x6126bb` -> `exit complete`
  6. 最终构造本地事件描述块，经 session 虚表 `+0x100` 回投事件 `9`
- 这和壳层 `ExitComplete -> fixedEvent 9` 对得上，说明 `0x17b340` 更像退出流程里的“广播 + 收尾 + 回投完成事件”处理器。
- 它仍然停留在 `CTDXSession` 的状态编排层，并没有直接下沉到 `7709` 行情帧编码。

### 58. `0x17f4f0 = Dormancy`，内部创建的是 `CTAJob_Close`

- `0x17f4f0` 的关键类名与状态串现在也能直接对上：
  - `0x605d73` -> `CTAJob_Close`
  - `0x605a4d` -> `CheckConnect`
- 当前可恢复的主流程如下：
  1. 先检查 session 当前状态处理器槽位 `+0x60`
  2. 若不是默认实现，则先执行当前状态处理器，拿到后续状态值
  3. 再通过 session 虚表 `+0x78` 获取 `CTAJob_Close` 对象
  4. 若获取成功，则调用该对象虚表：
     - `+0x30`
     - 首槽 `+0x0`
  5. 成功路径会清 `this + 0x80`
  6. 若获取失败，则置 `this + 0x80 = 1`，并把 `CheckConnect` 写入 `this + 0x84` 一带
- 这表明 `Dormancy` 更像：
  - 进入休眠/挂起态时，主动构造一个 close job
  - 若 close job 取不到，就把内部状态切到 `CheckConnect`
- 因而它依旧是 session 生命周期管理逻辑，不是目标 `7709` 行情协议的实际编码层。

### 59. 当前边界进一步收紧：Exit / Dormancy 这组也可以从“候选发包点”中排除

- 到目前为止，`0x17f9b0` 下游以下处理器都已经更像状态机重逻辑，而不是最终 socket 编码：
  - `0x17b340` `InExit`
  - `0x17b7a0` `InExitStart`
  - `0x17ebc0` 连接失败恢复 / 换 host
  - `0x17f4f0` `Dormancy`
- 因而后续最值得继续优先下钻的点可以进一步收敛为：
  - `0x17f710`：事件 `0x11` 运行时桥接器
  - `0x17c440`：对象集合广播层
  - `0x17e390` 一带：更可能对上 `OnTime CO_LAZYCON` / `ReTime`
- 其中 `OnTime CO_LAZYCON`、`ReTime`、`TimingReConn` 这组字符串，比 Exit / Dormancy 更像会逼近真实的连接维持、重连判定和后续网络提交层。

### 60. `0x17e390` 不是 `OnTime/ReTime`，而是 `SetOpt` 内部的配置分派入口

- 继续展开 `0x17e390` 后，可以确认它不是定时器处理器，而是一条按 key 字符串分支的配置写入逻辑。
- 当前已经直接对上的 key 包括：
  - `ConnOption`
  - `LazyTimeOut`
  - `MaxReConTimes`
  - `ClientInfo`
  - `SessionName`
  - `OpenJobName`
- 这条函数的几个关键行为如下：
  - `LazyTimeOut` 分支会把数值写到 `this + 0x40c`
  - `MaxReConTimes` 分支会把数值写到 `this + 0x418`，默认兜底值是 `0x3c`
  - `ConnOption` 分支会解析字符串值，写入：
    - `this + 0x454`
    - `this + 0x458 = 1`
    - `this + 0x4dc = 0`
    并在条件不满足时触发 `0x177730(this, 0x19, 0)`
- 因而此前把 `0x17e390` 当作 `OnTime CO_LAZYCON / ReTime` 候选是偏差；它更像 `CTDXSession SetOpt` 的重实现段。

### 61. `ReTime` 目前只命中一个返回日志串的小 helper，不是主处理器

- 目前对 `CTDXSession ReTime Session=%p,Client=%p,Job=%p` 的直接交叉引用，仍只看到：
  - `0x377010`：单纯返回该字符串地址的极小 helper
- 这说明：
  - `ReTime` 的实际处理器还没有通过简单字符串交叉引用直接浮现
  - 当前不能把 `0x377010` 误判为真实逻辑实现

### 62. `0x17dbb0` 是更重的事件/重试协调器，已明显逼近运行时恢复链

- `0x17dbb0` 到 `0x17e35b` 这一大段逻辑，已经明显比前面的壳层函数更重，当前可恢复的主体特征有：
  1. 先按输入事件类型分流，事件 `0x10` 与 `0x17` 走一条特殊路径
  2. 从 `job + 0x10` 关联对象读取：
     - `ObjClsName`
     - `ErrType`
     - `ErrCode`
  3. 若 `ErrType != 7`，则清 `this + 0x4f4`，并继续走对象集合广播/通知链
  4. 若 `ErrType == 7 && ErrCode == 0x2711`，则进入特殊恢复分支：
     - 递增 `this + 0x4f4`
     - 与 `this + 0x4f6` 比较上限
     - 超限后调用 `0x177730(this, 0x0a, jobObj)`
     - 若 `this + 0x4e4 != 0`，则置 `this + 0x4e0 = 1`
  5. 中间还会复制 `this + 0x508/+0x510` 对象集合到临时缓冲区，并遍历集合，对对象虚表 `+0x28` 进行筛选与调用
  6. 命中对象后还会走一条带日志的通知路径，日志串起点在 `0x612158`
- 从这组行为看，`0x17dbb0` 已经很像：
  - 某个运行时错误/超时/重试阈值的协调器
  - 它管理重试计数、候选对象遍历、通知和后续事件回投
- 但它当前还不能直接命名为 `OnTime CO_LAZYCON` 或 `ReTime`；更稳妥的说法是：
  - 这是一条明显更接近“运行时恢复链”的重逻辑实现
  - 后续应继续优先追它与 `0x17f710 / 0x17ebc0` 的关系，而不是再回到 Exit / Dormancy / SetOpt 壳层

### 63. `0x17dbb0`、`0x17f710`、`0x17ebc0` 当前更像并列处理器，不是直接互调链

- 继续按交叉引用检查后，目前能确定：
  - `0x17f710` 和 `0x17ebc0` 都是由 `0x17cc50` 注册进 `CTDXSession` 分发表
  - 两者都由总调度器 `0x17f9b0` 直接分流调用：
    - `0x17fb90 -> call 0x17f710`
    - `0x17fc40 -> call 0x17ebc0`
  - 但当前没有看到 `0x17dbb0` 直接调用 `0x17f710` 或 `0x17ebc0`
- 这说明当前更合理的结构是：
  - `0x17dbb0`
  - `0x17f710`
  - `0x17ebc0`
  三者属于同一状态机里的并列重逻辑处理器，而不是简单的前后串行调用关系。

### 64. `0x17f710` 更具体地对应 `event 0x11 + CTAJob_CloseEx`

- 复看 `0x17f710` 后，可把它的主体语义进一步明确为：
  - 若 `event == 0x11`，先构造本地事件块，再经 session 虚表 `+0x100` 回投事件 `0x11`
  - 之后进入通用分支：
    - 锁 `this + 0x550`
    - 若 `this + 0x3f8 != 0`，则清零 `this + 0x3f8`
    - 通过 session 虚表 `+0x78` 获取一个 job
    - 该 job 类名已可直接对上：`0x605d80 -> CTAJob_CloseEx`
    - 获取成功时执行 job 虚表 `+0x30` 与首槽 `+0x0`
    - 获取失败时置 `this + 0x80 = 1`，并把 `CheckConnect` 写入 `this + 0x84` 一带
- 这说明 `0x17f710` 不是一般性的通知桥，而更像：
  - 围绕特殊事件 `0x11` 的 close/close-ex 执行桥接器
  - 它与 `0x17f4f0 (Dormancy -> CTAJob_Close)` 很相近，但级别更偏 `CloseEx`

### 65. `0x17ebc0` 使用的是连接选项字段，不直接消费 `0x17dbb0` 的重试计数

- 复看 `0x17ebc0` 后，可进一步补充：
  - 它确实继续操作：
    - `this + 0x450 / +0x452`
    - `this + 0x3f8`
    - `this + 0x80`
  - 但在为新 job 写值时，使用的键已经对上：
    - `0x6056ce -> UseBalance`
  - 它还会依据 `this + 0x454` 是否为 0 决定是否立即执行 job 的虚表 `+0x30/+0x0`
- 当前没有看到它直接读写 `0x17dbb0` 那组特殊恢复计数：
  - `this + 0x4f4`
  - `this + 0x4f6`
- 因而更合理的判断是：
  - `0x17ebc0` 偏“候选连接对象切换 + 连接选项灌入 + 重新触发 job”
  - `0x17dbb0` 偏“错误码/错误类型驱动的运行时恢复与阈值协调”
  - 两者相关，但不是同一个计数状态机的同一函数体

### 66. `0x17d9b0` 是 `CTDXSession Exit` 壳层，`0x17dbb0` 是其旁侧的独立重逻辑函数

- 继续补读 `0x17d9b0..0x17db00` 后，可以把 `Exit` 壳层再精确一点：
  - `0x17d9b0` 会在参数为 `0` 时直接走：
    - `0x177730(this, 7, 0)`
    - 清理 `this + 0x550`
    - 若 `this + 0x4e4 != 0` 则置 `this + 0x4e0 = 1`
  - 参数非 `0` 时，则会经：
    - `this + 0x3f0`
    - `this + 0x58`
    - `0x1817e0 / 0x1818a0`
    组合并同步一组对象状态，随后同样回到上述 `event 7` 的退出路径
- 当调试级别较高时，`0x17db00` 这一段只负责打印：
  - `CTDXSession Exit Session=%p,Client=%p`
  然后跳回 `0x17d9e9` 继续执行 `Exit` 壳层主逻辑。
- 因而：
  - `0x17d9b0`/`0x17db00` 已明确属于 `CTDXSession Exit` 壳层与其日志包装
  - `0x17dbb0` 不是这条壳层函数的主体，而是旁边另一条独立重逻辑实现

### 67. `0x17e384` 只是一个 `this -= 0x58` 的 thunk，尾调到 `0x17dbb0`

- `0x17e380` 这段现在已经可以直接解释：
  - `sub rdi, 0x58`
  - `jmp 0x17dbb0`
- 这说明 `0x17e384` 不是独立逻辑，只是一个 thunk：
  - 把某个嵌入子对象/成员对象的 `this` 指针回退 `0x58`
  - 再尾调到真正的处理器 `0x17dbb0`
- 这个形态非常像：
  - 某个内部子对象的回调/虚表入口
  - 最终回落到 `CTDXSession` 本体处理函数
- 因而对 `0x17dbb0` 更稳妥的结构判断是：
  - 它是 `CTDXSession` 本体上的一条独立重逻辑函数
  - 同时还暴露了一个由子对象入口回调进来的 thunk `0x17e384`

### 68. `0x1380c0` 是另一条同型 thunk：把 `this+0x58` 子对象还原回父对象，再取 `+0x3bc`

- 继续展开后可见：
  - `0x1380c0` 本身只有：
    - `sub rdi, 0x58`
    - 跳到 `0x1380b0`
  - `0x1380b0` 的主体则是：
    - `lea rax, [rdi + 0x3bc]`
    - `ret`
- 也就是说：
  - 若传入的是某个 `parent + 0x58` 子对象指针
  - `0x1380c0` 会把它还原回父对象
  - 再返回父对象内的 `+0x3bc` 成员地址
- 这说明 `this + 0x58` 并不是孤立对象，而是一个嵌入在更大父对象里的成员接口视图。

### 69. `0x1817e0 / 0x1818a0` 是一对容器同步辅助函数

- 这两个函数的形态很接近，当前更像对某个哈希桶/链表容器做“按 key 查找并摘挂节点”的辅助器：
  - `0x1817e0`
    - 先根据输入对象和容器参数做哈希/桶定位
    - 在链表中查找匹配项
    - 命中后把节点从原链里摘出，并挂到容器头部/活跃槽位
    - 必要时在引用计数或剩余计数归零时调用 `0x181340`
  - `0x1818a0`
    - 也是按 key 在桶/链表里查找节点
    - 命中后做同样的摘挂与计数更新
    - 必要时调用 `0x1813e0`
- 它们都不是协议编码逻辑，而是成员对象/上下文节点的容器同步器。

### 70. `0x17d9b0` 与 `0x179d30` 复用的是同一套“子对象 -> 父对象成员 -> 容器同步”桥接模式

- `0x17d9b0` 的非零参数分支，以及 `0x179d30` 这条独立函数，都呈现相同模式：

### 71. `CommitLoginSuccess` 本体已可精确压缩：它只负责置活跃位、投 `event 4`，以及在 `0x4f0 != 0` 时补一跳 `state 5`

- 直接按 `0x27c2f0` 指令看，`CTDXSession_CommitLoginSuccess` 的主路径现在已经可以精确写成：
  1. 锁 `this + 0x550`
  2. 置：
     - `this + 0x408 = 1`
     - `this + 0x404 = 1`
  3. 调 `0x177730(this, 4, 0)`
  4. 若 `this + 0x4e4 != 0`，则补置 `this + 0x4e0 = 1`
  5. 若 `this + 0x4f0 != 0`，则再调 `0x177e80(this, 5)`
- 这一步很关键，因为它把此前容易混在一起的两层逻辑拆开了：
  - `CommitLoginSuccess` 本体并不会自己枚举广播对象集合
  - 它更像“登录成功后的轻量状态切换入口”
- 顺着这一修正，`0x177730(this, event, payload)` 的角色也可以先收紧成一个工作命名：
  - `CTDXSession_DispatchStateEvent`
  - 证据基础是：它稳定吃 `event/payload` 形态的参数，被 `Connect/Exit*/AbortLoginOperator Exit/CommitLoginSuccess` 这类壳层复用，也会被 `0x177a40` 在 drain state-action 队列时再次下沉调用
  - 当前仍应把这个名字视为“职责命名”，而不是最终符号名定论
- 因而当前更稳的宏观图应更新为：
  - `CommitLoginSuccess = activate primary context + emit event 4 + optional state 5`
  - 更重的对象枚举/筛选/恢复链应放到 `0x17dbb0` 这一簇理解，而不是继续压在 `CommitLoginSuccess` 本体上

### 72. `0x17dbb0` 已坐实会直接枚举 `this + 0x508/+0x510` 广播对象集合，并按对象虚表 `+0x28` 做筛选

- 这次继续下钻 `0x27dbb0` 后，能补上一条此前缺的硬证据：
  - 它会先把 `this + 0x508/+0x510` 的对象指针数组复制到临时缓冲
  - 然后逐个取对象，调用对象虚表 `+0x28`
  - 其中 `0x237d50` 已能对上默认空实现 `BroadcastIfaceSlot28Default`
  - 也就是说，`0x17dbb0` 不只是“泛泛地通知一批对象”，而是在显式筛选“谁真正实现了 slot 0x28”
- 这使得 `0x17dbb0` 的角色进一步收紧成：
  - 一个围绕运行时恢复/错误路径工作的重协调器
  - 它会把 session 当前广播对象集合拿出来，筛出具备特定恢复/通知能力的观察者，再继续执行后续路径
- 这也反过来说明：
  - `this + 0x508` 这组对象集合不仅服务于 `BroadcastHandler(event 4/5)`
  - 同时也被 `RuntimeRecoveryCoordinator(0x17dbb0)` 直接复用
  - 因而“登录成功后的观察者集合是否已注册”仍然是当前 live probe 最可疑的缺口之一

### 73. `this + 0x58` 子接口现在可以更明确地当作“观察者集合管理 + 恢复桥”的对外面

- `0x887b88` 这张 `CTDXSession` 嵌入接口表已能稳定读成：
  - slot 19 -> `0x27ba30 -> 0x27b940`：注册广播对象
  - slot 20 -> `0x27bb50 -> 0x27ba40`：摘除广播对象
  - slot 21 -> `0x27bc00 -> 0x27bb60`：清空广播对象集合
  - slot 22 -> `0x27bd00 -> 0x27bc10`：导出 `this + 0x520` 挂起状态三元组
  - slot 23 -> `0x27e380 -> 0x27dbb0`：直接桥到运行时恢复协调器
- 因而 `session + 0x58` 的角色现在可以更精确地更新为：
  - 不只是“session 子接口视图”
  - 而是一个明确暴露了观察者集合管理、挂起状态导出、运行时恢复入口的对外接口面
- 对当前协议逆向主线的直接意义是：
  - 如果 `1f47/1f44/1f4c/1f80` 这类 fetch 请求确实依赖登录成功后的观察者/bridge 对象唤醒
  - 那么最值得继续找的，不再是一般性的 warmup 包，而是“谁拿到 `session + 0x58` 并调用 slot 19 把 fetch handle/bridge 对象注册进 `this + 0x508`”

### 74. `slot 19 -> 0x27b940` 的调用面更像跨模块虚调，不会留下直接 `call 0x27b940` 的 xref

- 继续直接反查 `0x27b940` 后，当前仍只看到：
  - `0x27ba30: sub rdi, 0x58; jmp 0x27b940`
- 这说明：
  - `0x27b940` 的真实外部使用面不是静态直接调用
  - 而是通过 `session + 0x58` 这张嵌入接口表的高槽位间接进入
- 结合 `0x887b88` 已恢复出的槽位关系，现在可以把这一点写得更精确：
  - `slot 19 / +0x98 -> 0x27ba30 -> 0x27b940`
  - 调用者若手里拿的是 `session + 0x58` 子接口指针，就会先经 `sub rdi, 0x58` 还原父 `CTDXSession*`
  - 再进入真正的 observer add helper
- 因而后续在别的模块里继续追注册链时，关键模式不应再是：
  - `call 0x27b940`
- 而应切到：
  - “谁持有 `session + 0x58`”
  - “谁对该对象做高槽位虚调”
  - “谁把某个 bridge/observer 指针作为第二参数传进去”

### 75. 当前最强注册链候选，已从“fetch handle 本体”上移到 `TPData_MsgWnd -> CAppCore hook/context 注册`

- 这轮重新把 `libtpdata.so` 上游链条和 `CAppCore` 接口槽位并在一起看后，当前最强候选已进一步收敛：
  - 不是 `CreateFetchDataHandle(Ex)` 刚创建出的 `0x2ddd98/0x2dded8` fetch handle 本体，直接去拿 `session + 0x58` 做注册
  - 更像是 `TPData_MsgWnd` 在确保 `CAppCore` 可用时，先把自己的 hook/client/context 注册进 `CAppCore`
  - 再由 `CAppCore` 内部的 SessionManager / CTDXSession 路径完成真正的 observer 挂接
- 当前支撑这一判断的硬证据是：
  - `TPData_MsgWnd_EnsureAppCoreAndLazyService (0x4b66a)` 会：
    - 通过 `TaApi_CreateAppCore` 创建 `CAppCore`
    - 随后调用 `CAppCore` vtable `+0x20 -> 0x132f30`
    - 实参对上：`rdi = CAppCore*`, `rsi = TPData_MsgWnd + 0xe0`
  - `0x132f30` 内部会把第二参数保存到 `this + 0x8`，并围绕：
    - `IEventHook`
    - `IMBClient`
    - `m_pISessionMag!=NULL&&pIEventHook!=NULL&&pIMBClient!=NULL`
    这些语义继续处理
- 因而当前更稳的解释应更新为：
  - `TPData_MsgWnd + 0xe0` 很可能不是普通临时参数
  - 而是提供给 `CAppCore` 的 hook / MB client / 回调桥对象
  - 这个对象或其派生服务，才更像后续被接到 `CTDXSession` 广播/恢复链上的真正观察者来源
- 这也解释了为什么此前“只盯 fetch handle 双表”始终差一截：
  - fetch handle 更像请求/结果处理对象
  - 而把 session-ready / broadcast / recovery 事件真正桥进 `OnGetSession / SendGetEvtNode / CreateFetchDataHandle(Ex)` 主线的，更可能是 `TPData_MsgWnd` 提供给 `CAppCore` 的 hook/context
- 对 live probe 主线的直接含义是：
  - 眼下最可疑的缺失前置状态，不是“还少某个 fetch 请求”
  - 而是“客户端侧 `TPData_MsgWnd -> CAppCore` 这类 hook/context 注册尚未发生，因此 `CTDXSession` 的 observer 集合没有被正确填充”

### 76. `0x132f30` 当前更应保守命名为 `CAppCore_InitializeBridgeAndConfigLike`，而不是 session 级 observer add

- 继续把 `TPData_MsgWnd -> CAppCore` 这条链与 `session + 0x58` 子接口分开后，当前可以把 `0x132f30` 的角色再压缩一层：
  - 它更像 `CAppCore` 侧的 hook/client/context 注册入口
  - 还不像直接把对象写进 `CTDXSession + 0x508` 的 session 级 observer add helper
- 当前支撑这一定性的硬证据是：
  - 实参形态稳定对上：`rdi = CAppCore*`, `rsi = TPData_MsgWnd + 0xe0`
  - 进入后会把第二参数保存到 `this + 0x8`
  - 处理过程中反复围绕：
    - `IEventHook`
    - `IMBClient`
    - `m_pISessionMag!=NULL&&pIEventHook!=NULL&&pIMBClient!=NULL`
    这些语义做判断
- 因而当前更稳的命名应更新为：
  - `0x132f30 ~= CAppCore_InitializeBridgeAndConfigLike`
  - 而不是“直接注册到 session observer 列表”
- 这一步很重要，因为它把两层职责分开了：
  - `CAppCore` 层负责收下 `TPData_MsgWnd` 提供的 hook/context
  - `SessionManager / CTDXSession` 层才更可能负责真正把桥对象挂到 `session + 0x508`

### 77. 当前最可能的缺口，已经从“网络 warmup”明确转成“客户端 hook 注册未发生”

- 结合目前所有已知证据，当前最强工作假设已经可以明确写成：
  1. `CommitLoginSuccess` 本身已经只是轻量状态切换入口
  2. `session + 0x58` 暴露了 observer add/remove 与 recovery 入口
  3. 但外部真正进入这条链的最可能上游，不是单个 fetch handle，而是：
     - `TPData_MsgWnd`
    - `CAppCore_InitializeBridgeAndConfigLike(0x132f30)`
     - `CAppCore` 内部 SessionManager / CTDXSession
  4. 因而当前 Go probe 无法复现的最可疑缺口，不再是“还少哪个 warmup 包”
     - 而是“没有发生客户端侧的 hook/context 注册，所以 session observer 集合没有被正确填充”
- 对实验设计的直接含义是：
  - 继续盲试新的网络 warmup profile，预期收益已经很低
  - 更值得继续逆向的，是：
    - `0x132f30` 的内部虚调落点
    - `0x137e40` 懒取出来的 manager/service 对象
    - `TPData_MsgWnd + 0xe0` 的真实类型与虚表

### 78. `CAppCore` 这层当前可以先把 `+0x8 / +0x18` 两个关键字段稳定命名

- 这一节的旧版本曾把 `0x137e40` 直接解释为 `CAppCore` 的 manager/service getter；该结论已被后续真实反汇编推翻。
- 按当前最新证据，`CAppCore` 这层只能稳定留下一个高置信字段：
  - `CAppCore + 0x8`
    - 当前最合理的解释是：`hook/context bridge`
    - 来源：`0x132f30` 把 `TPData_MsgWnd + 0xe0` 直接保存到这里
    - 语义：更接近 `IEventHook / IMBClient / callback bridge` 一类对象，而不是 session 本体
- 当前不能再把 `CAppCore + 0x18` 稳定命名为 manager/service 入口；更稳妥的写法只能是：
  - `+0x8 = TPData_MsgWnd 提供给 CAppCore 的 hook/context 桥对象`
  - `+0x18 = 一个通过通用 getter 模板暴露出来的关联字段，具体对象类型待定`
- 因而当前从 `libtpdata` 到 `libtaapiw` 的上游链，应先缩回到：
  - `TPData_MsgWnd`
  - `TaApi_CreateAppCore -> CAppCore`
  - `CAppCore_InitializeBridgeAndConfigLike(0x132f30)`：把 `TPData_MsgWnd + 0xe0` 挂到 `CAppCore + 0x8`，并继续做 AppCore 侧接线与初始化
  - 其余 `CAppCore` 内部对象链仍需另外找独立证据坐实
- 对后续逆向优先级的直接影响是：
  - 当前不应再围绕 `0x137e40` 本身继续推 manager 语义
  - 更值得继续追的是 `0x132f30` 内部围绕 `CAppCore + 0x8/+0x50` 的真实虚调链

### 79. 下一步最值得继续追的单点，已经明确是 `0x137e40` 的返回对象

- 这一节的旧版本已经过期。
- 当前若只能选一个点继续深入，优先级不应再放在“`0x137e40` 返回什么对象”上，而应改成：
  - `0x132f30` 在保存 `CAppCore +0x8` 之后，还通过哪些对象和虚调把 `TPData_MsgWnd` 的上下文继续注册下去
- 原因是：
  - `0x137e40` 已经由真实反汇编坐实为“带日志保护的 `+0x18` 访问器”
  - 它不再提供足够的对象类型区分能力
  - `0x132f30` 才是当前仍与 `TPData_MsgWnd -> CAppCore` 初始化桥最直接相连的有效入口
- 因而当前最合理的下一跳应改写成：
  - `TPData_MsgWnd + 0xe0 -> CAppCore +0x8`
  - `0x132f30` 内部继续对这个 bridge/context 做哪些虚调或字符串匹配
  - 再判断它如何影响后续 `CTDXSession` observer/runtime 链

### 80. `0x137e40` 曾被怀疑是 `ISessionManager` 入口，但该判断已撤销

- 这一节已被后续真实反汇编否定，应视为历史推测，不再作为当前结论使用。
- 当前可替换成的最稳妥表述是：
  - `0x137e40` 只说明“某类对象把自己的 `+0x18` 字段通过带保护访问器暴露出来”
  - `TPData_MsgWnd +0x98` 也只能继续命名为“从 `CAppCore` vtable +0x158` 取得的附属对象”
  - 在没有这个附属对象的独立类型证据前，不能再命名成 `ISessionManager`

### 81. 当前结论已经足够支持：Go probe 缺的主因不是网络 warmup，而是本地 runtime/hook 初始化未发生

- 截至目前，这个判断已经可以从“强假设”提升为“高置信工作结论”：
  - Go probe 当前最可疑的缺口，不是普通网络 warmup 顺序
  - 而是 Linux 客户端会做的这一组本地初始化并未发生：
    - `TaApi_CreateAppCore`
    - `CAppCore_InitializeBridgeAndConfigLike(0x132f30)`
    - `TPData_MsgWnd` 侧对 `CAppCore` 派生附属对象的获取
    - 以及更后面的 `CTDXSession` / observer/runtime 本地对象初始化
- 这并不意味着后续完全不需要网络侧初始化；更准确的说法应是：
  - 当前缺口首先是“客户端本地 runtime 初始化未发生”
  - 在这一层没补齐前，继续盲试 warmup 包的收益已经明显偏低
- 因而当前最值得继续追的地址已经进一步明确成：
  - 第一优先：`0x132f30` 的内部虚调和其依赖对象
  - 第二优先：`TPData_MsgWnd +0x98` 那个附属对象的真实类型

### 82. `CTDXSessionIface_GetControlObject / GetDerivedService / GetActiveExternalObject` 这组三个 getter，已经能把 session 关键字段图谱定住

- 结合已知符号和文档中的 thunk/字段分析，这组三个 getter 当前可以稳定映射为：
  - `0x181220 = CTDXSessionIface_GetControlObject`
    - 返回：`this + 0x3f0`
    - 当前最合理解释：`control object / 上层宿主对象指针`
  - `0x181240 = CTDXSessionIface_GetDerivedService`
    - 返回：`this + 0x60`
    - 当前最合理解释：`derived service / factory / bridge 接口`
  - `0x181260 = CTDXSessionIface_GetActiveExternalObject`
    - 返回：`this + 0x68`
    - 当前最合理解释：`active external object` 的缓存槽位
- 同时，当前已能把它们与现有 helper 对上：
  - `0x17bd10 (CTDXSession_EnsureActiveExternalObject)`
    - 若 `+0x68` 为空，则通过 `+0x60` 的虚表 `+0x50` 懒取一个对象并缓存到 `+0x68`
  - `0x178430 (CTDXSession_BuildActionFromActiveContext)`
    - 当前更像围绕 `+0x3f0 / +0x60 / +0x68` 这组上下文字段组装下一步动作或派生对象
- 因而当前 session 关键字段图谱可以先稳定成：
  - `+0x3f0 = control object`
  - `+0x60 = derived service / factory`
  - `+0x68 = active external object cache`

### 83. `manager/service root -> CTDXSession -> session+0x58` 这条链的当前最佳模型

- 这一节的旧版本依赖 `0x137e40 == manager/service root getter`，现已不再成立。
- 当前保守到不出错的链条应改写成：
  1. `TPData_MsgWnd` 创建 `CAppCore`
  2. `0x132f30` 把 `TPData_MsgWnd +0xe0` 挂到 `CAppCore +0x8`
  3. `TPData_MsgWnd` 再通过 `CAppCore` 的其他槽位取得一个附属对象到 `+0x98`
  4. 这个附属对象如何走到 `CTDXSession` 仍待进一步坐实
  5. 但一旦进入 `CTDXSession`，它再通过：
     - `+0x3f0` control object
     - `+0x60` derived service
     - `+0x68` active external object
     这组三元关系继续推进后续 observer / runtime / action 流程
  6. 具体 observer add/remove/broadcast 再落到 `session +0x58`
- 因而当前对 `session +0x58` 的定位应再精确一点：
  - 它不是 manager 层对象
  - 而是具体 session 实例已经就位之后，对外暴露的 observer/broadcast/recovery 接口面

### 84. 当前已经可以高置信排除：observer 注册主要依赖网络 warmup

- 到这一轮为止，这个结论已经不仅仅是经验判断，而有比较完整的结构性支撑：
  - observer 集合相关接口由 `CTDXSession` 本地对象直接暴露：`session +0x58`
  - add/remove/clear helper 都是对象内逻辑：`0x27b940 / 0x27ba40 / 0x27bb60`
  - `CAppCore` 与 manager/service 的建立同样发生在客户端本地初始化阶段：
    - `TaApi_CreateAppCore`
    - `0x132f30`
    - `0x137e40`
  - `CTDXSession_EnsureActiveExternalObject` 也是围绕对象字段 `+0x60/+0x68` 做本地懒加载
- 因而当前最稳的说法应是：
  - 网络 warmup 也许仍影响某些后续状态
  - 但 observer 注册链本身，当前更像是 manager/session/runtime 初始化问题，而不是由某个额外网络包直接触发
- 这也进一步明确了后续主战场：
  - 不再是“猜还有哪几个 warmup 包没发”
  - 而是“manager 的哪个虚表槽位真正返回/构造了 `CTDXSession`，以及谁首次拿到了 `session +0x58`”

### 85. `+0x3f0 -> +0x60 -> +0x68` 这条链现在已经可以压成一个更稳定的类型模型

- 结合 `0x179186`、默认 getter `0x137e20 / 0x181170`、`0x17bd10`、`0x178430`，当前可以把这条链的类型语义进一步稳定成：
  - `CTDXSession +0x3f0`
    - `control object / 上层宿主对象`
  - `CTDXSession +0x60`
    - `service root / derived service / 下级 bridge 接口`
  - `CTDXSession +0x68`
    - `active external object cache / 运行期按需创建的活跃外部对象`
- 其中当前最关键的两条默认 getter 暗示了 `control object` 的内部布局：
  - 虚表 `+0x68` 的默认实现 `0x137e20`
    - 当前等价于：`return obj + 0x8`
    - 说明 control object 内部 `+0x8` 很可能就是其 service 指针槽
  - 虚表 `+0x78` 的默认实现 `0x181170`
    - 当前等价于：`return obj + 0x18`
    - 说明 control object 内部 `+0x18` 很可能就是其上下文对象/关键元数据槽
- 因而当前最优的类型链命名草案可以写成：
  - `CTDXSession.controlObject (+0x3f0)`
  - `CTDXControlObject.service (+0x8)`
  - `CTDXControlObject.contextObject (+0x18)`
  - `CTDXSession.serviceRoot (+0x60)`
  - `CTDXSession.activeExternalObject (+0x68)`

### 86. `manager -> session` 之后的内部结构模型，现在可以收紧为“control object 驱动 session 下游对象树”

- 这一节保留“session 内部对象树”的结论，但去掉前置的 manager 强命名。
- 当前最合理的模型应更新为：
  1. 某个上游附属对象最终创建或返回具体 `CTDXSession`
  2. session 初始化时挂入 `control object` 到 `+0x3f0`
  3. session 再从该 control object 导出 `serviceRoot` 到 `+0x60`
  4. session 通过 `serviceRoot@+0x50` 按需创建 `activeExternalObject` 到 `+0x68`
  5. session 再通过 `serviceRoot@+0x58`，结合：
     - `activeExternalObject(+0x68)`
     - `controlObject.contextObject(+0x18)`
     组装后续动作对象
  6. observer / runtime / action 链再继续推进到 `session +0x58`
- 这条模型的价值在于：
  - 它解释了为什么 `session +0x58` 不是 manager 层入口
  - 而是 session 内部对象树已经建立之后，对外暴露的状态机/observer/recovery 接口面

### 87. 下一步最值得继续追的单点，已从 `manager` 虚表进一步收敛到两个默认 getter

- 这一节也需要同步更新。
- 到当前为止，最值得继续追的优先级应统一成：
  - 第一优先：`0x132f30`
    - 目标：确认 `CAppCore +0x8` 挂入的 bridge/context 后续如何被消费
  - 第二优先：`TPData_MsgWnd +0x98`
    - 目标：确认这个从 `CAppCore` vtable `+0x158` 取得的附属对象真实类型，以及它是否参与 observer/runtime 注册链
  - 第三优先：`0x181170 / 0x137e20`
    - 目标：继续确认 `CTDXSession.controlObject` 的 `+0x18 / +0x8` 在后续动作链里的真实业务角色
- 换句话说，当前更细的主战场已经变成：
  - `TPData_MsgWnd -> CAppCore +0x8 bridge`
  - `TPData_MsgWnd +0x98` 附属对象
  - `CTDXSession.controlObject`
  - 三者之间到底由哪个附属对象或注册链条接通
  - `controlObject.service/context`
  - `session.serviceRoot -> activeExternalObject -> action`
  1. 拿一个对象的虚表 `+0xd0`
  2. 与 `0x1380c0` 比较
  3. 若相等，则调用 `0x1380c0`，拿到父对象的 `+0x3bc` 成员
  4. 随后调用：
     - `0x1817e0`
     - `0x1818a0`
     对 `this + 0x28` 和 `this + 0x58` 两组成员做同步
- 这说明：
  - `0x1380c0` 对应的是某个成员接口的统一 accessor
  - `0x17d9b0` 和 `0x179d30` 都在围绕同一类成员对象做“从子接口回到父对象，再同步容器状态”的操作

### 71. 这进一步支持：`0x17dbb0` 处理的是某类成员回调驱动的运行时恢复，而不是简单事件壳层

- 目前已看到的两条关键 thunk：
  - `0x17e384`：`this -= 0x58 -> 0x17dbb0`
  - `0x1380c0`：`this -= 0x58 -> parent + 0x3bc`
- 结合 `0x17d9b0` / `0x179d30` 对 `0x1380c0` 的重复使用，可以更稳妥地推断：
  - `CTDXSession` 内部至少有一个位于 `+0x58` 的成员接口/子对象
  - 该成员既能回调到 `0x17dbb0` 这类重逻辑处理器
  - 也能通过 `0x1380c0` 把自己映射回父对象内部另一个关键成员 `+0x3bc`
- 因而 `0x17dbb0` 更像是：
  - 一个由成员对象回调驱动的运行时恢复处理器
  - 后续继续识别这个 `+0x58` 子对象的类型，比单纯追日志串更有价值

### 72. `+0x58` 子对象当前更像“父对象字段 getter 接口”，而不是独立业务实体

- 继续看 `0x1380c0` 的兄弟 thunk 后，可再确认一项：
  - `0x138110` 也是同型 thunk：
    - `sub rdi, 0x58`
    - 返回父对象 `+0x404` 处的整型值
- 这说明同一个 `+0x58` 子对象接口，至少暴露了两类父对象字段：
  - `0x1380c0` -> `parent + 0x3bc`
  - `0x138110` -> `parent + 0x404`
- 当前更合理的解释是：
  - `+0x58` 不是完整业务对象本体
  - 更像一个嵌入式成员接口/适配器
  - 其方法实际只是把请求转回父对象的若干字段

### 73. `+0x3bc` 很可能是名字/标识字段，`+0x404` 更像状态/标志字段

- 已看到的两个调用场景说明这两个字段用途不同：
  - `0x1347e7 -> 0x1380c0` 返回值随后直接参与 `strcmp`
    - 说明 `parent + 0x3bc` 很可能是字符串指针或类名/标识名字段
  - `0x131af3 -> 0x138110` 返回值随后被 `test eax, eax`
    - 说明 `parent + 0x404` 更像整型状态值、布尔可用位或计数标志
- 再结合 `0x17dbb0`、`0x17d9b0` 对该接口的使用，可以更稳妥地说：
  - 这组子对象回调不是直接承载协议数据
  - 而是在为 session 内部的名字匹配、对象筛选、状态判定和恢复调度服务

### 74. `0x1817e0 / 0x1818a0` 在这条链上承担的是“成员同步/重挂”而不是业务判定

- 两个典型调用场景已经对上：
  - `0x17d9b0` 的非零参数分支
  - `0x179d30`
- 这两处都是：
  1. 通过 `0x1380c0` / `0x138110` 这类 getter thunk 从 `+0x58` 子接口回取父对象字段
  2. 再调用 `0x1817e0 / 0x1818a0`
  3. 把结果同步到 `this + 0x28` 和 `this + 0x58` 所代表的容器/成员结构中
- 因而对后续逆向的意义是：
  - 不必再把 `0x1817e0 / 0x1818a0` 当作高价值业务逻辑点
  - 真正高价值的仍然是：
    - `+0x58` 子接口本身的真实类型
    - 父对象 `+0x3bc` / `+0x404` 这组字段在更上层类中的业务命名

  ### 75. `+0x3bc` 现在基本可以视为名称缓冲区，而不是普通整数槽位

  - 当前已能看到多处一致证据：
    - `0x1791fd` 直接取 `this + 0x3bc`，随后调用字符串写入函数 `0xa5ed0`
    - `0x17c94b` 也把 `this + 0x3bc` 作为目标地址传入字符串填充逻辑
    - `0x1347e7 -> 0x1380c0` 取回 `parent + 0x3bc` 后，返回值立即参与 `strcmp`
    - `0x213dd6` 在构造阶段把 `+0x3bc/+0x3c0/+0x3c4/+0x3c8` 这一整段清零
  - 从这些行为看，`+0x3bc` 明显更像：
    - 内联字符串缓冲区的起点
    - 或者对象名称 / 标识名称字段的头部
  - 当前不再适合把它理解成普通整型状态位。

  ### 76. `+0x404` 更像构造期注入的外部上下文/句柄槽位，后续常被当作真假条件测试

  - 对 `+0x404` 的观察需要修正为更谨慎的版本：
    - `0x138110` 的确把 `parent + 0x404` 按 `int` 读出
    - 但 `0x213e0f` 在构造阶段是直接：
      - `mov rdx, [..] -> this + 0x404`
      也就是把一个 64 位值/指针写进该槽位
  - 后续在 `CTDXSession` 相关路径里，常见用法是只取其低 32 位测试真假：
    - `0x17f258` 读取 `this + 0x404` 后 `test r14d, r14d`
    - `0x17f440` 同样读取 `this + 0x404` 的低 32 位做分支判断
    - `0x17b020 / 0x17c2f0` 一带还会把它直接清零或置 1
  - 因而当前更稳妥的结论是：
    - `+0x404` 不是单纯的布尔位
    - 更像一个“外部上下文 / 句柄 / 指针大小槽位”
    - 只是许多路径把它当作是否存在某种连接上下文的真假位来测试

  ### 77. `CTDXSession` 内部已出现一组围绕名称缓冲区和连接上下文的相邻字段

  - 从 `0x213d80` 这一构造段看，`+0x3bc` 和 `+0x404` 并不是孤立字段，而是位于同一个更大的成员簇中：
    - `+0x3bc..+0x3c8`：一段被整体清零的名称/文本区
    - `+0x3f8..+0x400`：后续在连接状态机里频繁出现的连接状态位
    - `+0x404`：外部上下文/句柄槽位
    - `+0x40c`：另一段小型文本区，默认被写入 `Call`
    - `+0x424/+0x428/+0x42c/+0x420`：后续被用作桶大小、链表/容器、计数等结构参数
  - 这进一步支持：
    - `+0x58` 子接口回指的父对象，不是简单回调代理
    - 而是一个同时持有“对象名称、外部上下文、连接状态、容器参数”的核心运行时对象

  ### 78. `0x213c2e..0x21408d` 显示这不是零散字段，而是一整个运行时对象簇初始化

  - 把范围向前扩后，可以看到这一段并不只初始化 `+0x3bc/+0x404`：
    - `+0x2e0/+0x394/+0x3cc/+0x404/+0x43c` 都会在构造期吃同一个 `rdx`
    - 多段内联文本区被写入固定标签：
      - `+0x2ac = "reads"`
      - `+0x31c = "CORE:App..."`
      - `+0x474 = "NIO:E0:R..."`
      - `+0x4ac = "NIO:E0:S..."`
      - `+0x51c = "NIO:E1:R..."`
      - 后续还会继续落 `NIO:E1:S...`
  - 这说明当前命中的父对象，至少同时持有：
    - 名称/标识区
    - 多个外部上下文/句柄槽位
    - 多组连接/IO 统计或计数描述字段
  - 因而它更像一个“带 client/session/IO 统计描述的核心运行时对象”，而不只是单纯状态机壳层。

  ### 79. `+0x58` 不是只暴露 getter，而是一整套嵌入式成员接口

  - 目前已确认多组 `sub rdi, 0x58` thunk，不止 `0x1380c0/0x138110/0x17e384`：
    - `0x17bfc0 -> 0x17bed0`
    - `0x17c0c0 -> 0x17bfd0`
    - `0x17c1c0 -> 0x17c0d0`
    - `0x17c2e0 -> 0x17c1d0`
    - `0x17dba0 -> 0x17d9b0`
    - `0x17ebb0 -> 0x17e390`
    - 以及更早的 `0x178350/0x1784d0/0x1796a0/0x1796d0/0x17ab70` 等
  - 这表明：
    - `CTDXSession` 的一大批状态处理器，实际上同时暴露为“父对象直接方法”和“+0x58 成员接口方法”两种入口
    - `+0x58` 视图不是只读字段桥，而是一个可以承载完整状态机回调/通知/控制方法的嵌入接口对象

  ### 80. `0x1380d0` 这类桥接函数说明成员接口支持“默认 thunk 或真实虚实现”双态分派

  - `0x1380d0` 的逻辑不是简单 getter：
    - 先取 `this+0x50`
    - 再读虚表 `+0xd0`
    - 若该槽仍等于 `0x1380c0`，就回退到默认 thunk 路径
    - 否则直接跳到真实虚函数实现
  - 这意味着 `+0x58` 接口的宿主对象支持两种模式：
    - 没有专门 override 时，回落到父对象字段 getter
    - 有 override 时，直接转入真实实现
  - 从工程结构看，这更像一个正式接口类，而不是临时拼出来的偏移技巧。

  ### 81. `+0x404` 进一步收敛到“当前活跃 client/连接上下文”一类槽位

  - 新证据有两组：
    - `0x17c26e` 会在 `AbortLoginOperator Exit` 路径里显式把 `this+0x404` 清零，然后再下发事件 `5`
    - `0x137fe0..0x138060` 附近 rodata 明确出现：
      - `m_pIDataModel!=NULL`
      - `m_pISessionMag!=NULL`
      - `m_pISession!=NULL`
      - `m_pCurActiveClient!=NULL`
  - 结合此前：
    - `0x213e0f` 构造时把外部 `rdx` 写入 `+0x404`
    - `0x17f258/0x17f440` 在连接路径里只测其低 32 位真假
  - 当前最稳妥的定性更新为：
    - `+0x404` 很可能不是泛化“状态位”
    - 而是“当前活跃 client / 当前连接上下文”一类的指针大小槽位
    - 运行时很多路径仅把它是否为空作为分支条件

  ### 82. 某个 `+0x58` 嵌入接口 vtable 已直接挂上 `0x17f9b0` 总调度器

  - `0x7879d0` 这张 vtable 当前已经能对上多项已命名函数：
    - `0x1798b0`
    - `0x1799b0`
    - `0x1784e0`
    - `0x179d30`
    - `0x17f9b0`
    - `0x1793f0`
  - 其中 `0x17f9b0` 正是前面已坐实的 `CTDXSession` 总调度器。
  - 这意味着：
    - `+0x58` 视图不是外围辅助小接口
    - 而是直接承载了状态机核心调度能力的一张正式接口 vtable
  - 换句话说，`CTDXSession` 很可能确实把核心状态机能力通过 `+0x58` 这个嵌入成员接口对外暴露。

  ### 83. `0x1798b0 / 0x1799b0 / 0x1793f0` 进一步说明这张接口表还负责对象簇清理和重置

  - `0x1798b0` / `0x1799b0` 都会先把对象 vtable 指回 `0x7879d0`，然后清理：
    - `+0x330` 指针
    - `+0x88/+0x90/+0xa0/+0xa8` 一组对象/链表字段
    - `+0x58` 与 `+0x28` 两组成员容器
  - `0x1793f0` 也会操作：
    - `+0x520`
    - `+0x530/+0x538`
    - `+0x540`
    并把对象 vtable/子 vtable 切回固定值
  - 这支持当前判断：
    - `+0x58` 不是临时回调桥
    - 它属于同一个核心运行时对象的正式接口面，连生命周期清理都一起管了

  ### 84. `+0x404` 现在应与 `+0x408` 一起看，它们是并列判定字段

  - 新读点 `0x1801c0` 很关键：
    - 先读 `this + 0x404`
    - 若为 0，再读 `this + 0x408`
    - 两者都为 0 才走失败分支
  - 这说明：
    - `+0x404` 不是孤立状态位
    - 它和 `+0x408` 更像一组并列的“当前 client / 备用上下文 / 次级连接状态”字段
  - 结合前面 `AbortLoginOperator Exit` 会把 `+0x404` 清零，可以初步推断：
    - `+0x404` 偏向当前活跃 client/连接上下文
    - `+0x408` 可能是与之配套的次级上下文或状态标志
    - 下一轮应把 `+0x408` 的全部使用点补齐后再最终命名

  ### 85. `0x7879d0` 接口表已暴露一组成体系的状态/上下文 getter

  - 继续展开 `0x181140..0x181290` 后，可以把一批 `this -= 0x58` getter 明确对应到父对象字段：
    - `0x1811b0 -> 0x1811a0 -> return this+0x408`
    - `0x1811d0 -> 0x1811c0 -> return this+0x3f8`
    - `0x1811f0 -> 0x1811e0 -> return this+0x3fc`
    - `0x181210 -> 0x181200 -> return this+0x4e0`
    - `0x181230 -> 0x181220 -> return this+0x3f0`
    - `0x181250 -> 0x181240 -> return this+0x60`
    - `0x181270 -> 0x181260 -> return this+0x68`
  - 再结合先前已知的：
    - `0x138110 -> return this+0x404`
    - `0x1380c0 -> return this+0x3bc`
  - 当前可以把 `0x7879d0` 理解为：
    - 一张围绕 `CTDXSession`/其宿主运行时对象的“状态、连接上下文、名称、底层对象指针”接口表
    - 而不只是单一事件回调表

  ### 86. `+0x404/+0x408/+0x3f8/+0x3fc/+0x4e0` 很可能是一组连接可用性/活跃上下文判定字段

  - `0x1801c0` 已证实：
    - 先读 `+0x404`
    - 若为 0，再读 `+0x408`
    - 两者都为 0 才进入失败分支
  - `+0x404/+0x408` 的写法也呈并列状态：
    - `0x178f90 / 0x178f9a` 同时清 `+0x404/+0x408`
    - `0x17c337 / 0x17c341` 同时把 `+0x408/+0x404` 置 `1`
  - 而在接口表里，它们又与：
    - `+0x3f8/+0x3fc`（连接中标志）
    - `+0x4e0`
    - `+0x3f0`
    被并排暴露
  - 所以当前最稳妥的更新是：
    - `+0x404` 高概率是当前活跃 client / 主连接上下文
    - `+0x408` 高概率是与之并列的次级上下文或备用可用位
    - `+0x3f8/+0x3fc/+0x4e0/+0x3f0` 则属于同一组连接状态/控制对象成员

  ### 87. `0x17f9b0` 的参数形态也符合“成员接口把请求重新分派回 CTDXSession 处理器”

  - `0x17f9b0` 入口参数形态稳定为：
    - `rdi = this`
    - `rsi = handler function ptr`
    - `ecx/edx/r8 = event / code / payload` 一类运行时参数
  - 它所做的事情就是：
    - 对比 `rsi` 是否等于 `CreateJob/InExecute/RevcJob/ConnectIn/DisConnCpl/InNotify/0x17c440/0x17ebc0/0x17f710/0x17b340/0x17b7a0/0x17f4f0` 等已命名处理器
    - 命中后再把参数重排并回调到对应处理器本体
  - 这和 `0x7879d0` 里直接挂 `0x17f9b0` 是一致的：
    - `+0x58` 接口持有的并不是“另一个业务层”
    - 而是把外部事件/回调重新导回 `CTDXSession` 状态机本体的一层正式接口

  ### 88. `+0x3f0 -> +0x60 -> +0x68` 现在已经能串成一条更具体的对象链

  - `0x179186` 明确把 `rbp` 写入 `this + 0x3f0`，说明 `+0x3f0` 不是普通状态位，而是一个上层控制对象指针。
  - 紧接着 `0x179197..0x1791af`：
    - 先取该控制对象虚表 `+0x68`
    - 若槽位等于 `0x137e20`（即 `return obj+0x8` 的简单 getter）
    - 则把控制对象 `+0x8` 直接写到 `this + 0x60`
  - 因而当前最稳妥的解释是：
    - `+0x3f0`：控制对象/上层宿主对象指针
    - `+0x60`：从该控制对象派生出来的下级接口或服务指针

  ### 89. `+0x68` 更像运行期创建并缓存的活跃外部对象，而不是静态配置字段

  - `0x17bd18..0x17bd8e` 很关键：
    - 先读 `this + 0x68`
    - 若已非空则直接返回
    - 若为空，则取 `this + 0x60`
    - 再从其虚表 `+0x50` 取工厂方法
    - 调用后把返回对象写回 `this + 0x68`
  - 这说明 `+0x68` 不是简单配置字段，而是：
    - 经由 `+0x60` 所指下级接口按需创建/获取
    - 再缓存在 session 宿主对象上的活跃外部对象句柄
  - 同时这也解释了为什么在 `CTDXSession` 各处理器中，`+0x68` 会被频繁拿去参与日志、通知和后续动作。

  ### 90. `0x178430` 表明 `+0x60` 是真正负责“取外部动作对象”的下级接口

  - `0x178430` 的调用关系可以概括为：
    - 先取 `this + 0x60`
    - 从其虚表 `+0x58` 取方法指针
    - 再检查 `this + 0x3f0` 对应控制对象虚表 `+0x78`
    - 若该槽是默认 getter `0x181170(return obj+0x18)`，就直接取控制对象 `+0x18`
    - 然后把 `this + 0x68`、若干参数和这个控制对象值一起传给前面取到的 `+0x60` 方法
  - 从行为上看，`+0x60` 更像：
    - 下级 service / factory / bridge 接口
    - 它负责根据当前活跃对象 `+0x68` 和控制对象上下文，生成一个后续动作对象
  - 该动作对象随后还会：
    - 调其虚表 `+0x8`
    - 也就是再次被包装/通知后才返回给上层

  ### 91. `+0x404` 和 `+0x408` 的职责已经开始分化

  - 新增两个使用现场支持更细的分工：
    - `0x17cf99..0x17cfe3` 的超时/重连分支里，控制是否继续往外打日志/通知的是 `+0x408`
    - `0x17c337/0x17c341` 与 `0x178f90/0x178f9a` 又表明 `+0x404/+0x408` 总是成对置位或清零
  - 当前更合理的中间结论是：
    - `+0x404` 偏“主连接上下文/当前活跃 client 存在性”
    - `+0x408` 偏“当前上下文是否允许继续重试/继续通知”的并列活跃状态
  - 这两个字段还不能完全最终命名，但已经不适合再混成同一个泛化布尔概念。

  ### 92. `RuntimeRecoveryCoordinator` 已直接证明 `+0x3f0` control object 参与恢复链，但 `0x27bc10` 不能再当作 control object 证据

  - `0x27dbb0 (CTDXSession_RuntimeRecoveryCoordinator)` 在 `0x27e1f0..0x27e1fd` 出现了一条当前非常关键的调用链：
    - 先取 `RAX = this(CTDXSession)`
    - 再做 `MOV RDI, [RAX + 0x3f0]`
    - 然后把 `R13` 作为第二参数传入
    - 最后 `CALL qword ptr [RAX + 0x60]`，其中此时的 `RAX` 已被改写为 `control object` 的虚表
  - 这说明：
    - `+0x3f0` 作为 `control object` 的判断进一步被实锤
    - 恢复链里确实会直接回调 `control object` 的虚表 `+0x60`
    - 因而 `control object` 不是单纯静态配置容器，而是恢复/运行时流程中的主动参与者
  - 但同时要明确排除一个容易混淆的旧线索：
    - `0x27bc10 (SessionEmbeddedIfaceSlot22Body)` 读取的是 `this + 0x520`
    - 它只是把 `qword[+0x10/+0x18/+0x20]` 从 `+0x520` 指向的挂起状态对象拷到调用者缓冲
    - 这条路径描述的是“导出挂起状态三元组”，不能再被拿来佐证 `control object +0x18/+0x20`
  - 因而当前证据应分成两类看：
    - `control object` 的稳定证据：`+0x3f0`、`0x181220`、`0x27e1f0..0x27e1fd`、`0x179186`
    - `slot22 / 0x27bc10` 的稳定证据：`session +0x520` 挂起状态导出 helper

  ### 93. `0x137e20 / 0x181170` 目前只能证明 `session +0x58` 接口会复用 control-style getter，尚不能把它们当成 control object vtable 全貌

  - 到目前为止，`0x137e20 / 0x281160 / 0x181170` 的直接落点只在 `0x8879d0 (CTDXSessionIface58)` 这张表上看见：
    - slot 13 -> `0x137e20`
    - slot 14 -> `0x281160`
    - slot 15 -> `0x181170 (CTDXControl_DefaultGetContextObject)`
  - 这意味着当前最稳妥的说法应更新为：
    - `session +0x58` 这张嵌入接口表复用了几项 control-style getter
    - 但仅凭这张表，还不能说它已经等价展示了 `control object` 自身 vtable 的全貌
  - 因而对两个默认 getter 的解释要保持收敛：
    - `0x137e20` 仍只可保守理解为“返回宿主对象 `+0x8` 的简单 getter”
    - `0x181170` 仍只可保守理解为“返回宿主对象 `+0x18` 的简单 getter”
    - 它们支持 `control object` 内部存在 `service/context` 两个关键槽位的模型
    - 但还不足以直接证明 `+0x8` 一定是 `session manager`，或 `+0x18` 一定是 `observer owner`
  - 当前更可靠的命名层级仍应保持在：
    - `CTDXControlObject.serviceLike (+0x8)`
    - `CTDXControlObject.contextLike (+0x18)`
    - 等后续找到 `0x137e20` / `0x181170` 的函数体或更多交叉用例，再决定是否进一步收紧命名

  ### 94. `RuntimeRecoveryCoordinator` 末尾的 `+0x78` 访问属于 `CTDXSession` 自身字段，不应再混入 `0x181170` 语义

  - `0x27dbb0` 在 `0x27dd23..0x27dd31` 的收尾段是：
    - `MOV RAX, [RBP - 0x118]`
    - `MOV RDI, [RAX + 0x78]`
    - `MOV RAX, [RDI]`
    - `CALL qword ptr [RAX]`
  - 这里的 `RAX` 仍然是 `CTDXSession* this`，所以 `+0x78` 是 `session` 本体字段，而不是前面默认 getter 所说的 `control object` 虚表 `+0x78`。
  - 这说明：
    - `RuntimeRecoveryCoordinator` 里至少并存两条不同的对象访问路径：
      - `session +0x3f0 -> control object -> vtable +0x60`
      - `session +0x78 -> 某个 session-owned helper object -> vtable +0x0`
    - 因而不能把 `0x27dd2a` 这段再拿来支持 `0x181170` 或 `control object.contextLike(+0x18)` 的解释
  - 当前更稳妥的阶段性结论是：
    - `0x181170` 的证据来源仍应限于默认 getter 本身及其在 `session +0x58` 嵌入接口表中的复用
    - `0x27dd2a` 只说明 `CTDXSession` 自身还挂着一个位于 `+0x78` 的辅助对象，恢复流末尾会调用它的首槽方法

  ### 95. `0x137e40` 需要降级重评：它在现有实锤用例里更像“默认返回对象 `+0x18`”的虚方法，而不是已坐实的 manager 懒加载入口

  - 新读点 `0x231920 (ObserverDerivedBBody30)` 给出了一条比旧推断更直接的模式：
    - 先取 `RBP = [RDI + 0x48]`
    - 再取该对象虚表 `+0x158`
    - 若该槽位等于 `0x237e40`，则不再虚调，而是直接检查并取 `RBP + 0x18`
    - 否则才走 `CALL RAX`
  - 这类写法和前面多处“若槽位等于默认 getter，则直接展开字段访问”的模式完全一致。
  - 因而到当前为止，`0x137e40` 至少可以高置信理解成：
    - 一个返回宿主对象 `+0x18` 的默认虚方法，或与之等价的极薄 getter
  - 这会直接影响前面若干关于 `0x137e40` 的命名强度：
    - 先前把它高置信命名为 `CAppCore_GetLazySessionManagerLike`
    - 现在必须降级为“尚未坐实，且已出现与默认 `+0x18` getter 高度一致的反例”
  - 当前更稳妥的更新应是：
    - `0x137e40` 不能再被当作已坐实的 manager/service 懒加载入口
    - 至少在 `0x231920` 这条 observer 派生链里，它表现为典型的默认 getter 比较目标
    - 后续若还要保留 `CAppCore +0x18` 那条主线，必须拿出 `0x137e40` 函数体或其他独立调用点来重新支撑

  ### 96. `0x237d00` 已坐实是“返回宿主对象 `+0x8`”的极简默认 getter，说明 `0x237d00..0x237e40` 一带确实存在默认接口模板簇

  - `.ghidra/out/libtaapiw_237d00_237d80_abs.txt` 已直接给出：
    - `0x237d00: LEA RAX, [RDI + 0x8]; RET`
  - 同区间与相邻 vtable 还出现了多条非常薄的默认实现：
    - `0x237d10 / 0x237d20 / 0x237d30 / 0x237d40 / 0x237d50` 多为 `xor eax,eax; ret`
    - `ObserverIfaceDerivedA/B` 等 vtable 直接挂这些模板函数
  - 因而当前可以更有把握地说：
    - `0x237d00..0x237e40` 这一带本身就是“默认接口槽实现”的高密度区域
    - 这进一步增强了前一节对 `0x137e40` 的重新判断：它更像模板 getter，而不是复杂业务入口

  ### 97. 真实二进制与 `.ghidra/out` 导出当前存在固定 `-0x100000` 坐标差，后续读 `objdump` 必须先做地址换算

  - 本轮直接对 `/workspace/Software/com.tdx.tdxcfv_7.64_amd64/opt/apps/com.tdx.tdxcfv/files/lib64/tdx/libtaapiw.so` 做 `objdump` 后，已确认：
    - `.ghidra/out` 里的 `0x237d00` 对应实际二进制 `0x137d00`
    - `.ghidra/out` 里的 `0x281170` 对应实际二进制 `0x181170`
  - 对齐锚点如下：
    - 实际 `0x137d00` 的函数体确实是 `lea rax, [rdi+0x8]; ret`
    - 实际 `0x181170` 的函数体确实是 `mov rax, [rdi+0x18]; ret`
  - 因而当前这一批导出文本，可以稳定按下面规则换算：
    - `actual_objdump_addr = ghidra_export_addr - 0x100000`
  - 这条规则非常重要，因为前面一度把 `0x237e40` 直接拿去跑 `objdump`，结果误读成了别的函数中段。

  ### 98. `0x137e20 / 0x181160 / 0x181170` 已由真实函数体坐实为三条极简字段 getter

  - 真实二进制反汇编已确认：
    - `0x137e20`: `mov rax, [rdi+0x8]; ret`
    - `0x181160`: `mov rax, [rdi+0x10]; ret`
    - `0x181170`: `mov rax, [rdi+0x18]; ret`
  - 这意味着前面围绕默认 getter 的几条推断里，以下部分现在已经可以升级为实锤：
    - `0x137e20` 就是“返回宿主对象 `+0x8`”
    - `0x181170 (CTDXControl_DefaultGetContextObject)` 就是“返回宿主对象 `+0x18`”
    - `0x181160` 则补出了中间槽位：它是“返回宿主对象 `+0x10`”
  - 因而若把这一组三个槽并起来看，当前最稳妥的宿主对象字段模型应写成：
    - `+0x8  = serviceLike / first associated object`
    - `+0x10 = second associated object`
    - `+0x18 = contextLike / lazy-associated object`
  - 其中 `+0x18` 之所以仍先命名成 `contextLike` 而不是更强的业务名，是因为“字段来源/生命周期”虽然坐实了，但“真实业务角色”仍需由调用点继续界定。

  ### 99. `0x137e40` 不是简单 getter，而是“带日志保护的 `return obj+0x18` 访问器”；`0x137f10` 是其 `+0x10` 对偶

  - 真实二进制反汇编已把 `0x137e40` 的函数体完整读开：
    - 先取 `rax = [rdi+0x18]`
    - 若非空则直接返回
    - 若为空，则走一段统一日志/诊断路径（`0x4a` 号消息）
    - 日志后再次读取 `[rbx+0x18]` 返回
  - 同时相邻的 `0x137f10` 也呈现完全同型的结构：
    - 先取 `rax = [rdi+0x10]`
    - 若为空则走统一日志/诊断路径（`0x4b` 号消息）
    - 最后返回 `[rbx+0x10]`
  - 这说明：
    - `0x137e40` 确实不是“复杂 manager 懒初始化器”
    - 它更像“带空值告警的 `+0x18` 访问器”
    - `0x137f10` 则是同一家族里的“带空值告警的 `+0x10` 访问器”
  - 结合上一节的极简 getter，可以把这一家族总结成：
    - 简单模板：`0x137e20 / 0x181160 / 0x181170`
    - 带保护模板：`0x137f10(+0x10)`、`0x137e40(+0x18)`
  - 因而前面把 `0x137e40` 高置信命名为 `CAppCore_GetLazySessionManagerLike` 的那条链，现在可以正式撤销。

  ### 100. 不能再把“虚表槽位命中 `0x137e40`”直接等同于“对象就是 `CAppCore`”

  - 本轮结合 `0x231920 (ObserverDerivedBBody30)` 和真实反汇编后，可以把一个容易继续误导分析的点单独钉住：
    - `0x231920` 里被取出并比较 `vtable + 0x158` 的对象，位于 observer 管理对象的 `this + 0x48`
    - 命中 `0x137e40` 后，代码直接取该对象 `+0x18`
    - 随后又对这个 `+0x18` 指向对象调用其虚表 `+0x38`，并把 `CTDXSession` 名字传进去
  - 这条用法说明：
    - `0x137e40` 是一条“可被多类对象复用”的字段访问器
    - 仅凭某对象虚表某槽位等于 `0x137e40`，还不能反推出该对象一定是 `CAppCore`
    - 更不能再把“命中 `0x137e40`”自动解释成“在取 `ISessionManager`”
  - 当前更稳妥的读法应是：
    - `0x137e40` 只描述“这个类把某个 `+0x18` 字段通过带保护 getter 暴露出来”
    - 至于 `+0x18` 对具体类意味着什么，必须回到该类自己的调用上下文和对象布局来判断
  - 因而前面文档中凡是依赖“`0x137e40` == CAppCore manager getter”这个前提推出的强命名结论，当前都应视为历史假设，而不是已验证事实。

  ### 101. `0x132f30` 现在更像 `CAppCore` 的“外部 bridge 接线 + 配置/日志初始化”入口，而不只是单纯 RegisterEventHook

  - 本轮直接读真实二进制 `0x132f30` 后，已经可以把它的职责再收紧一层：
    - 它确实会在很早阶段把第二参数写入 `CAppCore +0x8`
    - 但后续并不是立刻走 session 创建，而是围绕多个字符串键、配置项、日志项和外部对象虚方法做一整段初始化
  - 当前能直接对上的字符串包括：
    - `syscfg.json`
    - `LogLevel`
    - `DataModule.log`
    - `syscfg/qscfg.ini`
    - `Public`
    - `CfgEncrypt`
    - `TAEngine/Log`
    - `DataCache`
    - 以及源码路径字符串：`.../SessionManager/AppCore.cpp`
  - 结合调用形态，当前最稳妥的过程模型是：
    1. `0x132f30` 保存外部对象到 `CAppCore +0x8`
    2. 通过该外部对象的虚表 `+0x0 / +0x10 / +0x38` 取若干字符串或上下文值
    3. 通过 `CAppCore +0x10` 指向的内部对象虚表 `+0x20` 取一个派生对象，结果写到 `CAppCore +0x20`
    4. 用 `Public/CfgEncrypt` 之类键去查询/设置布尔配置，结果写到 `CAppCore +0xcc`
    5. 再通过 `CAppCore` 的其他槽位取得对象到 `+0x28`，并围绕：
       - `TAEngine/Log`
       - `DataModule.log`
       - `DataCache`
       - `syscfg/qscfg.ini`
       做后续配置或日志初始化
  - 因而当前对 `0x132f30` 的命名应从：
    - `CAppCore_RegisterEventHookLike`
    调整为更稳妥的：
    - `CAppCore_InitializeBridgeAndConfigLike`
  - 这并不否定“它会把 `TPData_MsgWnd +0xe0` 作为 hook/context bridge 挂到 `+0x8`”这一点；只是说明它的真实职责比单纯注册 hook 更重，至少还包含：
    - 启动配置读取
    - 日志系统/DataModule 相关初始化
    - 若干 AppCore 内部派生对象的建立或接线
  - 对主线的直接影响是：
    - `TPData_MsgWnd -> CAppCore` 这一步本身就带一段本地 bootstrap 逻辑
    - 因而 Go probe 当前缺的，未必只是“observer 注册”这一件事
    - 也可能包括 AppCore 侧配置/日志/数据模块初始化未发生，导致后续 session/runtime 链条根本没被拉起