#!/usr/bin/env bash
set -euo pipefail

REMOTE_HOST="${REMOTE_HOST:?REMOTE_HOST is required}"
REMOTE_USER="${REMOTE_USER:-root}"
REMOTE_DIR="${REMOTE_DIR:-/opt/ops_terminator}"
REMOTE_BRANCH="${REMOTE_BRANCH:-$(git rev-parse --abbrev-ref HEAD)}"
REMOTE_PORT="${REMOTE_PORT:-7778}"
REMOTE_ADDR=":${REMOTE_PORT}"
REMOTE_SSH_OPTS="${REMOTE_SSH_OPTS:-}"
GIT_REMOTE_URL="${GIT_REMOTE_URL:-$(git remote get-url origin)}"
REMOTE_GIT_HTTP_PROXY="${REMOTE_GIT_HTTP_PROXY:-}"

OSAGENT_LLM_BASE_URL="${OSAGENT_LLM_BASE_URL:-https://api.hbyzn.cn}"
OSAGENT_LLM_MODEL="${OSAGENT_LLM_MODEL:-qwen3.6-plus}"
OSAGENT_EMBEDDING_MODEL="${OSAGENT_EMBEDDING_MODEL:-text-embedding-3-small}"
OSAGENT_LLM_API_KEY="${OSAGENT_LLM_API_KEY:?OSAGENT_LLM_API_KEY is required}"
REMOTE_GOPROXY="${REMOTE_GOPROXY:-https://goproxy.cn,direct}"

ssh_remote() {
  if [ -n "${SSHPASS:-}" ]; then
    # shellcheck disable=SC2086
    sshpass -e ssh ${REMOTE_SSH_OPTS} "${REMOTE_USER}@${REMOTE_HOST}" "$@"
    return
  fi
  # shellcheck disable=SC2086
  ssh ${REMOTE_SSH_OPTS} "${REMOTE_USER}@${REMOTE_HOST}" "$@"
}

ssh_remote "set -euo pipefail
export PATH=/usr/local/go/bin:\$PATH
export LANG=C
export LC_ALL=C
export GOPROXY='${REMOTE_GOPROXY}'
git_remote() {
  if [ -n '${REMOTE_GIT_HTTP_PROXY}' ]; then
    git -c http.proxy='${REMOTE_GIT_HTTP_PROXY}' -c https.proxy='${REMOTE_GIT_HTTP_PROXY}' \"\$@\"
    return
  fi
  git \"\$@\"
}
if ! command -v git >/dev/null 2>&1; then echo 'git missing on remote' >&2; exit 20; fi
if ! command -v go >/dev/null 2>&1; then echo 'go missing on remote' >&2; exit 21; fi
mkdir -p '${REMOTE_DIR}'
if [ ! -d '${REMOTE_DIR}/.git' ]; then
  git_remote clone '${GIT_REMOTE_URL}' '${REMOTE_DIR}'
fi
cd '${REMOTE_DIR}'
git_remote fetch origin '${REMOTE_BRANCH}'
git_remote checkout '${REMOTE_BRANCH}'
git_remote reset --hard FETCH_HEAD
if ss -ltn 2>/dev/null | awk '{print \$4}' | grep -qE '(^|:)${REMOTE_PORT}$'; then
  echo 'remote port ${REMOTE_PORT} is already occupied' >&2
  exit 22
fi
cat > .env <<'EOF'
OSAGENT_LLM_BASE_URL=${OSAGENT_LLM_BASE_URL}
OSAGENT_LLM_API_KEY=${OSAGENT_LLM_API_KEY}
OSAGENT_LLM_MODEL=${OSAGENT_LLM_MODEL}
OSAGENT_EMBEDDING_MODEL=${OSAGENT_EMBEDDING_MODEL}
OSAGENT_SERVER_ADDR=${REMOTE_ADDR}
OSAGENT_DATA_DIR=data
OSAGENT_REQUEST_TIMEOUT_SECONDS=120
OSAGENT_RUN_TIMEOUT_SECONDS=180
OSAGENT_KNOWN_HOSTS=
LANG=C
LC_ALL=C
EOF
go test ./...
go build ./...
go build -o /tmp/ops-terminator-test ./cmd/osagent
cat > /etc/systemd/system/ops-terminator-test.service <<EOF
[Unit]
Description=ops_terminator remote validation service
After=network.target

[Service]
Type=simple
WorkingDirectory=${REMOTE_DIR}
EnvironmentFile=${REMOTE_DIR}/.env
Environment=LANG=C
Environment=LC_ALL=C
ExecStart=/tmp/ops-terminator-test serve
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl restart ops-terminator-test.service
for i in \$(seq 1 30); do
  if curl -fsS 'http://127.0.0.1:${REMOTE_PORT}/api/health' >/tmp/ops_terminator_health.json; then break; fi
  sleep 1
done
cat /tmp/ops_terminator_health.json
curl -fsS 'http://127.0.0.1:${REMOTE_PORT}/api/settings/policy' >/tmp/ops_terminator_policy.json
curl -fsS -X POST 'http://127.0.0.1:${REMOTE_PORT}/api/knowledge' -H 'Content-Type: application/json' -d '{\"kind\":\"sop\",\"status\":\"active\",\"scope\":\"global\",\"title\":\"Disk pressure triage SOP\",\"body\":\"Use df -h and df -i first, then inspect large directories. Do not delete files before confirming impact.\",\"tags\":[\"disk\",\"df\",\"inode\"]}' >/tmp/ops_terminator_sop.json
RUN_ID=\$(curl -fsS -X POST 'http://127.0.0.1:${REMOTE_PORT}/api/runs' -H 'Content-Type: application/json' -d '{\"host_id\":\"local\",\"user_input\":\"请用只读命令检查磁盘空间，并说明是否命中了磁盘 SOP\",\"requested_by\":\"remote_validation\"}' | sed -n 's/.*\"id\":\"\\([^\"]*\\)\".*/\\1/p')
if [ -z \"\$RUN_ID\" ]; then echo 'failed to create run' >&2; exit 23; fi
for i in \$(seq 1 90); do
  STATUS=\$(curl -fsS \"http://127.0.0.1:${REMOTE_PORT}/api/runs/\$RUN_ID\" | sed -n 's/.*\"status\":\"\\([^\"]*\\)\".*/\\1/p')
  case \"\$STATUS\" in
    completed) break ;;
    failed|denied) curl -fsS \"http://127.0.0.1:${REMOTE_PORT}/api/runs/\$RUN_ID\"; exit 24 ;;
  esac
  sleep 2
done
curl -fsS \"http://127.0.0.1:${REMOTE_PORT}/api/runs/\$RUN_ID\" >/tmp/ops_terminator_run.json
curl -fsS \"http://127.0.0.1:${REMOTE_PORT}/api/knowledge\" >/tmp/ops_terminator_knowledge.json
curl -fsS \"http://127.0.0.1:${REMOTE_PORT}/api/sessions?limit=5\" >/tmp/ops_terminator_sessions_limited.json
curl -fsS \"http://127.0.0.1:${REMOTE_PORT}/api/sessions/\$(sed -n 's/.*\"session_id\":\"\\([^\"]*\\)\".*/\\1/p' /tmp/ops_terminator_run.json)?turn_limit=5\\&events_limit=20\\&compact=true\" >/tmp/ops_terminator_session_detail_limited.json
echo \"REMOTE_VALIDATION_RUN_ID=\$RUN_ID\"
cat /tmp/ops_terminator_run.json
"
