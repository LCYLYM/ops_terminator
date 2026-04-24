# ops terminal

ops terminal 是面向 Linux 运维场景的统一操作控制台。系统把主机接入、会话、执行、审批、事件审计和实时回放整合到一条稳定链路中，支持本机与 SSH 主机的真实执行。

## 产品定位

- 统一入口：Web 与 CLI 共用同一套 gateway、策略和执行链路。
- 真实执行：所有主机探测、命令执行和审批处理都基于真实 API 与真实运行环境。
- 风险控制：只读操作直接执行，变更类操作进入人工审批，危险命令按策略拒绝。
- 可追溯：完整保留 `host / session / turn / run / approval / event / audit` 记录。
- 可扩展：内置工具、策略规则、主机类型和执行后端保持模块化。

## 核心能力

### 主机管理

- 支持 `local` 与 `ssh` 主机。
- SSH 主机支持独立地址、端口、用户、认证信息和主机状态管理。
- 会话可绑定指定主机，避免跨主机误操作。

### 会话控制

- 以 session 为核心组织每次运维交互。
- 每轮请求生成可追踪的 turn / run 记录。
- 对话主视图展示执行过程、工具结果、审批状态和最终输出。
- 侧边栏提供会话概览、实时事件和运行状态。

### 执行与工具

主执行路径为 `run_shell`，同时内置常用运维工具：

- `host_probe`
- `memory_inspect`
- `disk_inspect`
- `directory_usage_inspect`
- `port_inspect`
- `process_search`
- `service_status_inspect`
- `file_log_search`
- `journal_log_search`
- `package_manager_inspect`
- `user_inspect`
- `create_user`
- `delete_user`
- `restart_service`
- `run_shell`

命令结果统一返回 `command / exit_code / duration / stdout / stderr` 结构，超长输出会按配置截断，保证界面和上下文稳定。

### 审批与策略

- 显式只读工具和只读 shell 命令直接放行。
- 可能修改系统状态的 shell 或 builtin 进入人工审批。
- 复杂绕过语法、嵌套解释器和明显破坏性命令直接拒绝。
- 审批卡片支持展开、收起、批准、强制批准和拒绝。
- 审批处理结果会写入事件流和审计记录。

### 实时控制台

- Web 主界面包含会话列表、对话主视图、审批条、主机选择、输入区和 Live Trace。
- 全局 SSE 推送运行状态、工具事件、审批事件和最终结果。
- 历史会话支持回放，便于复盘执行过程。

## 运行

1. 复制环境文件：

```bash
cp .env.example .env
```

2. 写入真实模型服务配置：

```bash
OSAGENT_LLM_API_KEY=...
OSAGENT_LLM_BASE_URL=...
OSAGENT_LLM_MODEL=...
```

3. 启动服务：

```bash
go run ./cmd/osagent serve
```

4. 打开控制台：

```text
http://127.0.0.1:7778
```

如果 `.env` 中配置了 `OSAGENT_SERVER_ADDR`，以实际配置端口为准。

## CLI 示例

```bash
go run ./cmd/osagent hosts
go run ./cmd/osagent host-add --id local --name 本机 --mode local
go run ./cmd/osagent run --host local --input "请检查当前磁盘空间和 inode 使用情况"
go run ./cmd/osagent approvals
go run ./cmd/osagent approve --id <approval-id> --decision approve
```

## 验证

```bash
go test ./...
go build ./...
```

## 已验证链路

- 本机与 SSH 主机执行链路
- 会话、运行、审批、事件和审计记录
- 只读运维操作直接执行
- 高风险操作人工审批
- 历史会话详情回放
- 全局 SSE 实时事件流
