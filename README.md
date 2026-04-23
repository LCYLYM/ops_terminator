# Product MVP


## 目标

- 真实 LongCat thinking 模型/qwen模型
- 流式 tool-calling agent loop
- 统一 gateway
- `host / session / turn / run / approval / event / audit`
- 本地与 SSH 真实执行
- Web 与 CLI 共用一条控制面路径
- 面向 Linux 运维场景的内置工具与风控
- 主要是web端

## 当前能力

- 主机管理：`local` / `ssh`
- 会话控制面：`session / turn / run / approval / event / audit`
- Web 主界面：
  - 左侧会话列表
  - 中间 ChatGPT 风格对话主视图
  - 对话中展示 tool / policy / approval 卡片
  - 右侧审批列表、run 列表、live trace
  - 全局 SSE 实时刷新
- 运维 builtins：
  - 主执行路径：`run_shell`
  - 高价值快捷能力：
  - `hello_capability`
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
- policy：
  - 只读工具直接放行
  - `run_shell` 先做安全解析：显式只读命令直接放行
  - 可能改写系统状态的 shell 或 builtin 统一进入人工审批
  - 复杂绕过语法、嵌套解释器、明显破坏性 shell 直接拒绝
  - 命令结果统一返回 `command / exit_code / duration / stdout / stderr` 结构，超长输出自动截断
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

页面不是日志页，而是对话主视图；`Live Trace` 只作为辅助日志面板。

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

## 已验证链路

- 真实 LongCat 流式/tool-calling
- 只读运维闭环
- 高风险审批闭环
- `session detail` 历史回放接口
- 全局 SSE 实时事件流
