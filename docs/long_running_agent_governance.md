# ops_terminator 长期安全与记忆体系

本文档定义 ops_terminator 后续长期自动推进的四条主线：AI 命令安全审查、AI 操作记忆与偏好、操作员偏好、过往报错和成功案例 SOP 复用。所有能力必须基于真实 run、真实工具结果、真实审批和真实 audit，不允许使用 mock 数据替代。

## 1. 安全审查

Policy Engine 输出统一的规则结果：

- `rule_id`：稳定规则 ID。
- `category`：`shell` 或 `builtin`。
- `severity`：`low`、`medium`、`high`、`critical`。
- `decision`：`allow`、`ask`、`deny`。
- `reason`：面向操作员的解释。
- `safer_alternative`：更安全的替代路径。
- `override_allowed`：是否允许受控 override。

默认规则资产记录在 `configs/policies/shell_command_safety.json`，首次启动后会落到运行时数据目录的 `policy_config.json`，并可通过系统设置页和 `/api/settings/policy` 编辑。运行时仍由 Go 代码执行解析和判定，避免把安全边界交给模型自由解释；受保护的 deny 规则不能在 UI/API 中放宽。审批记录、事件流、audit 和前端卡片必须携带规则 ID。

第一批规则覆盖：

- 显式只读 builtin 或 shell 命令直接放行。
- 可能修改系统状态的 builtin 或 shell 命令进入审批。
- `rm -rf /`、格式化、关机、重启、原始块设备写入等破坏性命令直接拒绝。
- `curl ... | sh`、`wget ... | bash` 直接拒绝。
- `bash -c`、`python -c`、`node -e` 等嵌套解释器直接拒绝。
- here-doc、命令替换、后台执行、进程替换等复杂 shell 语法直接拒绝。

## 2. AI 长期记忆

Session memory 只解决单会话连续性。长期知识使用独立 `KnowledgeItem`，类型包括：

- `memory`：从真实 run 中产生的候选记忆。
- `preference`：已确认偏好，不替代 operator profile。
- `sop`：可复用操作流程。
- `incident`：失败、报错或恢复案例。

每条知识必须有来源字段，例如 `source_run_id`、`source_turn_id`、`source_event_id` 或 `source_sop_id`。模型或运行时生成的新知识默认是 `pending`，不能直接成为事实；只有人工确认或明确的系统规则提升后才能变为 `active`。

Embedding 只用于检索排序，不作为事实来源。若 embeddings endpoint 不可用，系统必须记录失败并降级到关键词检索，不能伪造向量结果。

## 3. 操作员偏好

操作员偏好存放在独立 operator profile，不混入 rolling summary。当前字段包括：

- `approval_strictness`
- `allow_bypass_approvals`
- `allow_force_approve`
- `allow_plaintext_ssh_warning`
- `allow_automation_bypass`
- `prefer_read_only_first`
- `remote_validation_required`

这些偏好会注入 agent 上下文，用来影响模型选择更保守或更自动化的路径。偏好变更必须写入 audit。

## 4. SOP 与案例复用

SOP 的来源有两类：

- `configs/skills/*.json` 中的运维流程。
- 从真实 run、approval、event、audit 中抽取并去敏后的长期知识。

Agent 执行前最多注入 3 条相关 SOP 或 active knowledge。回答和 audit 需要保留来源 ID，避免把经验变成无法追踪的模型臆测。

## 5. 验收标准

每个阶段必须满足：

- 远端 `go test ./...` 通过。
- 远端 `go build ./...` 通过。
- 远端服务 `/api/health` 返回 `status=ok`。
- 至少一个真实 run 完成，并产生 `run / turn / event / audit`。
- 安全规则 smoke 覆盖 allow、ask、deny 三类。
- 长期知识候选默认 pending。
- 操作员偏好变更写入 audit。
- SOP 检索能在真实 run 前注入上下文。
