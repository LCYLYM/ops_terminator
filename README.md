# Product MVP

这是基于当前最小 agent 闭环重新拉起的独立产品目录，不继承旧 `260422比赛超聚变/mvp` 的实现代码，只继承需求边界。

## 目标

- 真实 LongCat thinking 模型
- 流式 tool-calling agent loop
- 统一 gateway
- `host / session / turn / run / approval / event / audit`
- 本地与 SSH 真实执行
- Web 与 CLI 共用一条控制面路径
- 面向 Linux 运维场景的内置工具与风控

## 当前能力

- 主机管理：`local` / `ssh`
- 运维 builtins：
  - `hello_capability`
  - `host_probe`
  - `memory_inspect`
  - `disk_inspect`
  - `port_inspect`
  - `process_search`
  - `service_status_inspect`
  - `file_log_search`
  - `create_user`
  - `delete_user`
  - `restart_service`
  - `run_shell`
- policy：
  - 只读工具直接放行
  - 变更类工具人工审批
  - 明显破坏性 shell 直接拒绝
- Web 控制面：host 管理、run 发起、审批处理、事件流回放

## 运行

1. 复制环境文件：

```bash
cp .env.example .env
```

2. 写入真实 LongCat key：

```bash
OSAGENT_LLM_API_KEY=...
```

3. 启动服务：

```bash
go run ./cmd/osagent serve
```

4. 打开：

```text
http://127.0.0.1:7788
```

## CLI 示例

```bash
go run ./cmd/osagent hosts
go run ./cmd/osagent host-add --id local --name 本机 --mode local
go run ./cmd/osagent run --host local --input "请检查当前磁盘空间和 inode 使用情况"
go run ./cmd/osagent approvals
go run ./cmd/osagent approve --id <approval-id> --decision approve
```

## 测试

```bash
go test ./...
go build ./...
```
