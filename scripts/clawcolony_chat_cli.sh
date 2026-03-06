#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
SENDER="${SENDER:-clawcolony-admin}"

if ! command -v curl >/dev/null 2>&1; then
  echo "curl not found"
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "jq not found"
  exit 1
fi

print_menu() {
  cat <<'EOF'

=== Clawcolony Chat CLI ===
1) 列出 AI Bots
2) 发送点对点消息 (Clawcolony -> CLAW)
3) 发送全体广播
4) 查看全部聊天历史 (最近 20 条)
5) 查看某个目标的 direct 历史
0) 退出
EOF
}

while true; do
  print_menu
  read -r -p "选择操作: " action
  case "${action}" in
    1)
      curl -fsS "${BASE_URL}/v1/bots" \
        | jq -r '.items[] | "- \(.bot_id) | name=\(.name // "") | provider=\(.provider // "") | status=\(.status // "")"'
      ;;
    2)
      read -r -p "目标 bot_id: " receiver
      read -r -p "消息内容: " content
      curl -fsS -X POST "${BASE_URL}/v1/chat/send" \
        -H "Content-Type: application/json" \
        -d "{\"sender\":\"${SENDER}\",\"receiver\":\"${receiver}\",\"content\":\"${content}\",\"wait_reply\":true}" \
        | jq .
      ;;
    3)
      read -r -p "广播内容: " content
      curl -fsS -X POST "${BASE_URL}/v1/chat/broadcast" \
        -H "Content-Type: application/json" \
        -d "{\"sender\":\"${SENDER}\",\"content\":\"${content}\"}" \
        | jq .
      ;;
    4)
      curl -fsS "${BASE_URL}/v1/chat/history?limit=20" \
        | jq -r '.items[] | "[\(.id)] [\(.target_type)] \(.sender) -> \(.target): \(.content)"'
      ;;
    5)
      read -r -p "target (bot_id or clawcolony-admin): " target
      curl -fsS "${BASE_URL}/v1/chat/history?target_type=direct&target=${target}&limit=20" \
        | jq -r '.items[] | "[\(.id)] [\(.target_type)] \(.sender) -> \(.target): \(.content)"'
      ;;
    0)
      echo "bye"
      exit 0
      ;;
    *)
      echo "无效输入"
      ;;
  esac
done
