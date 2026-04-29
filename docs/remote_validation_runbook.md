# 远端验证 Runbook

ops_terminator 的长期推进必须在远端服务器完成验证。本机只用于编辑、提交和推送，不作为测试环境。

## 1. 输入配置

远端验证脚本从环境变量读取配置：

```bash
export REMOTE_HOST=<remote-host>
export REMOTE_USER=root
export REMOTE_DIR=/opt/ops_terminator
export REMOTE_BRANCH=codex/long-running-governance
export REMOTE_PORT=7778
export OSAGENT_LLM_BASE_URL=https://api.hbyzn.cn
export OSAGENT_LLM_MODEL=qwen3.6-plus
export OSAGENT_EMBEDDING_MODEL=text-embedding-3-small
export OSAGENT_LLM_API_KEY=<runtime-secret>
```

API key、SSH 密码和其他凭据不得写入 git、README、PR 描述或测试日志摘要。

## 2. 固定流程

```bash
git push -u origin codex/long-running-governance
bash scripts/remote_validate.sh
```

脚本执行以下真实步骤：

1. 远端检查 `git` 和 `go`。
2. 远端 clone 或更新 GitHub 分支。
3. 远端写入未跟踪 `.env`。
4. 远端执行 `go test ./...`。
5. 远端执行 `go build ./...`。
6. 写入 `ops-terminator-test.service`。
7. 重启 systemd 服务。
8. 调用 `/api/health`。
9. 创建 active SOP。
10. 发起真实 agent run。
11. 拉取 run 和 knowledge 结果。

远端端口固定为 `:7778`。如果端口被占用，验证失败并退出，不能自动换端口。

## 3. PR 记录

PR 描述必须包含：

- 远端 `go test ./...` 摘要。
- 远端 `go build ./...` 摘要。
- `/api/health` 摘要。
- 真实 run ID。
- 是否发生 embedding 降级。
- 剩余风险。

不得包含 API key、SSH 密码、`.env` 内容或服务器私有日志。
