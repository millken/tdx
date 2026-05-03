# tdxrpc64.dll 协议分析报告

## 1. 核心流程分析

`tdxrpc64.dll` 的协议组装遵循以下逻辑链条：

1.  **业务请求触发**：如 `rpcClientConnect` 启动连接。
2.  **内部 Command ID 映射** (地址: `0x18009d1b0`)：
    *   根据输入参数（如 `0x54` 或 `0x6c`）选择 Command ID。
    *   **Connect**: `0x54` -> `0x0d` (存入 `[rcx+0x3c]`)
    *   **Heartbeat**: `0x6c` -> `0x04` (存入 `[rcx+0x3c]`)
3.  **记录结构初始化**：Command ID 进一步转换为 Packet Type。
4.  **20 字节头部组装** (地址: `0x180014cd0`)：
    *   从记录结构偏移 `+0x8` 读取 Packet Type。
    *   写入 Buffer 的第 2 个字节 (index 1)。

## 2. 字段偏移定义 (x64)

| 偏移 | 大小 | 说明 |
| :--- | :--- | :--- |
| `+0x8` | 1 Byte | Packet Type (在 20 字节头部的 Index 1) |
| `+0x3c` | 4 Bytes | Internal Command ID |
| `+0xd0` | - | 缓冲区/数据区起始位置 |

## 3. 已知 Command ID 映射

| 业务请求码 | Command ID (Hex) | 功能 |
| :--- | :--- | :--- |
| `0x54` | `0x0d` | Connect (连接/认证) |
### Header 数据结构 (20 字节)

基于对 `tdxrpc64.dll` 函数 `0x180014cd0` 的分析，协议头部固定为 20 字节，其构建过程如下：

| 偏移 (Offset) | 长度 (Size) | 描述 (Description) | 指令来源 (Assembly) |
| :--- | :--- | :--- | :--- |
| `0x00` | `1` | **Magic/Prefix**: 固定为 `0x27` | `mov BYTE PTR [rsp+0x28], 0x27` |
| `0x01` | `1` | **Packet Type**: 命令类型 | `movzx eax, BYTE PTR [rax+0x8]` -> `mov [rsp+0x29], al` |
| `0x02` | `2` | **Unknown/Reserved**: 填充 | 栈初始化 |
| `0x04` | `4` | **Sequence/ID**: 会话序列号 | `mov eax, [rax+0x248]` -> `mov [rsp+0x2c], eax` |
| `0x08` | `8` | **Body Pointer (?)**: 内存地址 | `mov rax, [rax]` -> `mov [rsp+0x30], rax` |
| `0x10` | `4` | **Body Length**: Payload 长度 | `mov eax, [rax+0x20]` -> `mov [rsp+0x38], eax` |

**注1**: 在 DLL 内部，`0x08` 偏移处是一个 64 位指针 `[rsp+0x30]`。但在网络发包时，该位置可能被转换为数据偏移或保留位。根据 `rep movs` 指令，这 20 字节被完整拷贝到外发缓冲区 (`rax+0x400`)。

#### 发包逻辑分析 (0x180014cd0)
该函数作为最终的数据包装器 (Wrapper)：
1. 在栈上 `[rsp+0x28]` 分配 20 字节临时空间并初始化。
2. 将 `0x27` 写入首字节。
3. 从记录结构体（由 `rax` 指向）提取 `Type`、`Sequence`、`DataPointer` 和 `DataLength`。
4. 使用 `rep movs` (20 字节) 将这块内存拷贝到目的缓冲区。
| `0x49` | `0x08` | (待证, 疑似行情/查询) |

## 4. 验证计划 (TODO)
- [ ] 编写 Golang Mock Client 发送 `0x0d` 包头。
- [ ] 观察服务器返回的包体结构。
- [ ] 扫描 `0x18009d1b0` 附近的 switch-case 分支，提取全部常量映关系。
