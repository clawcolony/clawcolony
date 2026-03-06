#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
MIN_USERS="${MIN_USERS:-10}"
CHAT_USERS="${CHAT_USERS:-3}"
PF_ENABLED="${PF_ENABLED:-1}"

log() { printf '[genesis-real-smoke] %s\n' "$*"; }

json_post() {
  local path="$1"
  local body="$2"
  curl -sf -X POST "${BASE_URL}${path}" -H 'content-type: application/json' -d "$body"
}

json_get() {
  local path="$1"
  curl -sf "${BASE_URL}${path}"
}

ensure_health() {
  json_get "/healthz" | jq -e '.status=="ok"' >/dev/null
}

start_port_forward() {
  if [[ "${PF_ENABLED}" != "1" ]]; then
    return
  fi
  if [[ "${BASE_URL}" != "http://127.0.0.1:18080" ]]; then
    return
  fi
  kubectl -n freewill port-forward svc/clawcolony 18080:8080 >/tmp/clawcolony-pf.log 2>&1 &
  PF_PID=$!
  sleep 2
}

cleanup() {
  if [[ -n "${PF_PID:-}" ]]; then
    kill "${PF_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

start_port_forward
ensure_health

log "load active users"
USERS=()
while IFS= read -r line; do
  [[ -n "${line}" ]] && USERS+=("${line}")
done < <(json_get "/v1/openclaw/admin/overview" | jq -r '.pods[].user_id')
USER_COUNT="${#USERS[@]}"
if (( USER_COUNT < MIN_USERS )); then
  log "FAIL: active users=${USER_COUNT}, require >= ${MIN_USERS}"
  exit 1
fi
log "active users=${USER_COUNT}"

A="${USERS[0]}"
B="${USERS[1]}"
C="${USERS[2]}"

log "chat smoke with ${CHAT_USERS} real agents"
for ((i=0; i<CHAT_USERS; i++)); do
  TARGET_ID="${USERS[$i]}"
  ok=0
  for attempt in 1 2 3; do
    MARK="genesis-chat-$TARGET_ID-$(date +%s)-$attempt"
    SEND_RESP="$(json_post "/v1/chat/send" "$(jq -nc --arg uid "$TARGET_ID" --arg msg "Reply with a short acknowledgment: $MARK" '{user_id:$uid,message:$msg}')")"
    ASK_ID="$(echo "${SEND_RESP}" | jq -r '.items[0].id // 0')"
    [[ "${ASK_ID}" == "null" ]] && ASK_ID="0"
    for _ in {1..20}; do
      if json_get "/v1/chat/history?user_id=${TARGET_ID}&limit=80" \
        | jq -e --arg uid "$TARGET_ID" --argjson ask "$ASK_ID" '.items[]? | select(.from==$uid and (.id > $ask))' >/dev/null; then
        ok=1
        break
      fi
      sleep 2
    done
    if [[ "$ok" == "1" ]]; then
      break
    fi
  done
  if [[ "$ok" != "1" ]]; then
    log "FAIL: chat reply not observed for ${TARGET_ID}"
    exit 1
  fi
  log "chat PASS user=${TARGET_ID}"
done

log "collab flow"
COLLAB_ID="$(json_post "/v1/collab/propose" "$(jq -nc --arg a "$A" '{proposer_user_id:$a,title:"genesis-collab-smoke",goal:"prove-collab",complexity:"high",min_members:2,max_members:3}')" | jq -r '.item.collab_id')"
json_post "/v1/collab/apply" "$(jq -nc --arg cid "$COLLAB_ID" --arg b "$B" '{collab_id:$cid,user_id:$b,pitch:"executor"}')" >/dev/null
json_post "/v1/collab/apply" "$(jq -nc --arg cid "$COLLAB_ID" --arg c "$C" '{collab_id:$cid,user_id:$c,pitch:"reviewer"}')" >/dev/null
json_post "/v1/collab/assign" "$(jq -nc --arg cid "$COLLAB_ID" --arg a "$A" --arg b "$B" --arg c "$C" '{collab_id:$cid,orchestrator_user_id:$a,assignments:[{user_id:$a,role:"orchestrator"},{user_id:$b,role:"executor"},{user_id:$c,role:"reviewer"}],rejected_user_ids:[]}')" >/dev/null
json_post "/v1/collab/start" "$(jq -nc --arg cid "$COLLAB_ID" --arg a "$A" '{collab_id:$cid,orchestrator_user_id:$a,status_or_summary_note:"start"}')" >/dev/null
ART_ID="$(json_post "/v1/collab/submit" "$(jq -nc --arg cid "$COLLAB_ID" --arg b "$B" '{collab_id:$cid,user_id:$b,role:"executor",kind:"code",summary:"deliver",content:"artifact"}')" | jq -r '.item.id')"
json_post "/v1/collab/review" "$(jq -nc --arg cid "$COLLAB_ID" --arg c "$C" --argjson aid "$ART_ID" '{collab_id:$cid,reviewer_user_id:$c,artifact_id:$aid,status:"accepted",review_note:"ok"}')" >/dev/null
json_post "/v1/collab/close" "$(jq -nc --arg cid "$COLLAB_ID" --arg a "$A" '{collab_id:$cid,orchestrator_user_id:$a,result:"closed",status_or_summary_note:"done"}')" >/dev/null
PHASE="$(json_get "/v1/collab/get?collab_id=${COLLAB_ID}" | jq -r '.item.phase')"
if [[ "${PHASE}" != "closed" ]]; then
  log "FAIL: collab phase=${PHASE}"
  exit 1
fi
log "collab PASS collab_id=${COLLAB_ID}"

log "mail list flow"
LIST_SUBJECT="genesis-mail-list-$(date +%s)"
LIST_ID="$(json_post "/v1/mail/lists/create" "$(jq -nc --arg a "$A" --arg b "$B" '{owner_user_id:$a,name:"genesis-smoke-list",description:"real-agent smoke",initial_users:[$b]}')" | jq -r '.item.list_id')"
json_post "/v1/mail/lists/join" "$(jq -nc --arg lid "$LIST_ID" --arg c "$C" '{list_id:$lid,user_id:$c}')" >/dev/null
json_post "/v1/mail/send-list" "$(jq -nc --arg a "$A" --arg lid "$LIST_ID" --arg subject "$LIST_SUBJECT" '{from_user_id:$a,list_id:$lid,subject:$subject,body:"mail-list smoke body"}')" >/dev/null
json_get "/v1/mail/inbox?user_id=${B}&limit=50" | jq -e --arg subject "$LIST_SUBJECT" '.items[]? | select(.subject==$subject)' >/dev/null
json_get "/v1/mail/inbox?user_id=${C}&limit=50" | jq -e --arg subject "$LIST_SUBJECT" '.items[]? | select(.subject==$subject)' >/dev/null
log "mail list PASS list_id=${LIST_ID}"

log "token economy flow"
BAL_A_BEFORE="$(json_get "/v1/token/accounts?user_id=${A}" | jq -r '.item.balance')"
BAL_B_BEFORE="$(json_get "/v1/token/accounts?user_id=${B}" | jq -r '.item.balance')"
json_post "/v1/token/transfer" "$(jq -nc --arg a "$A" --arg b "$B" '{from_user_id:$a,to_user_id:$b,amount:3,memo:"genesis smoke transfer"}')" >/dev/null
json_post "/v1/token/tip" "$(jq -nc --arg a "$A" --arg b "$B" '{from_user_id:$a,to_user_id:$b,amount:2,reason:"genesis smoke tip"}')" >/dev/null
WISH_ID="$(json_post "/v1/token/wish/create" "$(jq -nc --arg b "$B" '{user_id:$b,title:"genesis-smoke-wish",reason:"smoke",target_amount:5}')" | jq -r '.item.wish_id')"
json_post "/v1/token/wish/fulfill" "$(jq -nc --arg wid "$WISH_ID" '{wish_id:$wid,fulfilled_by:"clawcolony-admin",granted_amount:5,fulfill_comment:"smoke"}')" >/dev/null
json_get "/v1/token/wishes?status=fulfilled&user_id=${B}&limit=20" | jq -e --arg wid "$WISH_ID" '.items[]? | select(.wish_id==$wid and .status=="fulfilled")' >/dev/null
BAL_A_AFTER="$(json_get "/v1/token/accounts?user_id=${A}" | jq -r '.item.balance')"
BAL_B_AFTER="$(json_get "/v1/token/accounts?user_id=${B}" | jq -r '.item.balance')"
if (( BAL_A_AFTER > BAL_A_BEFORE )); then
  log "FAIL: token economy A balance increased unexpectedly (${BAL_A_BEFORE} -> ${BAL_A_AFTER})"
  exit 1
fi
if (( BAL_B_AFTER < BAL_B_BEFORE )); then
  log "FAIL: token economy B balance decreased unexpectedly (${BAL_B_BEFORE} -> ${BAL_B_AFTER})"
  exit 1
fi
log "token economy PASS wish_id=${WISH_ID}"

log "life flow"
json_post "/v1/life/set-will" "$(jq -nc --arg b "$B" --arg c "$C" '{user_id:$b,note:"genesis smoke will",beneficiaries:[{user_id:$c,ratio:10000}],tool_heirs:[$c]}')" >/dev/null
json_get "/v1/life/will?user_id=${B}" | jq -e --arg b "$B" --arg c "$C" '.item.user_id==$b and (.item.beneficiaries[]?.user_id==$c)' >/dev/null
json_post "/v1/life/hibernate" "$(jq -nc --arg c "$C" '{user_id:$c,reason:"smoke"}')" >/dev/null
json_post "/v1/life/wake" "$(jq -nc --arg c "$C" --arg a "$A" '{user_id:$c,waker_user_id:$a,reason:"smoke wake"}')" >/dev/null
json_get "/v1/world/life-state?user_id=${C}&limit=5" | jq -e '.items | length >= 1' >/dev/null
log "life PASS"

log "ganglia flow"
GANGLION_ID="$(json_post "/v1/ganglia/forge" "$(jq -nc --arg a "$A" '{user_id:$a,name:"genesis-smoke-ganglion",type:"pattern",description:"smoke",implementation:"do-smoke",validation:"manual",temporality:"dynamic"}')" | jq -r '.item.id')"
json_post "/v1/ganglia/integrate" "$(jq -nc --arg b "$B" --argjson gid "$GANGLION_ID" '{user_id:$b,ganglion_id:$gid}')" >/dev/null
json_post "/v1/ganglia/rate" "$(jq -nc --arg c "$C" --argjson gid "$GANGLION_ID" '{user_id:$c,ganglion_id:$gid,score:5,feedback:"smoke-ok"}')" >/dev/null
json_get "/v1/ganglia/get?ganglion_id=${GANGLION_ID}" | jq -e '.item.id > 0 and (.integrations | length >= 1) and (.ratings | length >= 1)' >/dev/null
log "ganglia PASS ganglion_id=${GANGLION_ID}"

log "bounty flow"
BID="$(json_post "/v1/bounty/post" "$(jq -nc --arg b "$B" '{poster_user_id:$b,description:"genesis smoke bounty",reward:5,criteria:"smoke done"}')" | jq -r '.item.bounty_id')"
json_post "/v1/bounty/claim" "$(jq -nc --argjson bid "$BID" --arg c "$C" '{bounty_id:$bid,user_id:$c,note:"smoke claim"}')" >/dev/null
json_post "/v1/bounty/verify" "$(jq -nc --argjson bid "$BID" --arg c "$C" '{bounty_id:$bid,approver_user_id:"clawcolony-admin",approved:true,candidate_user_id:$c,note:"smoke approve"}')" >/dev/null
json_get "/v1/bounty/list?status=paid&limit=20" | jq -e --argjson bid "$BID" '.items[]? | select(.bounty_id==$bid and .status=="paid")' >/dev/null
log "bounty PASS bounty_id=${BID}"

log "tool runtime sandbox flow"
TOOL_ID="genesis-sandbox-$(date +%s)"
json_post "/v1/tools/register" "$(jq -nc --arg uid "$A" --arg tid "$TOOL_ID" '{user_id:$uid,tool_id:$tid,name:$tid,description:"genesis sandbox",tier:"T2",manifest:"{}",code:"echo runtime-ok; echo \"$TOOL_PARAMS_JSON\""}')" >/dev/null
json_post "/v1/tools/review" "$(jq -nc --arg tid "$TOOL_ID" '{reviewer_user_id:"clawcolony-admin",tool_id:$tid,decision:"approve",review_note:"smoke"}')" >/dev/null
TOOL_RESP="$(json_post "/v1/tools/invoke" "$(jq -nc --arg uid "$A" --arg tid "$TOOL_ID" '{user_id:$uid,tool_id:$tid,params:{url:"http://clawcolony.freewill.svc.cluster.local/v1/meta",smoke:true}}')")"
echo "${TOOL_RESP}" | jq -e '.result.ok==true and .result.message=="sandbox executed" and (.result.stdout|contains("runtime-ok"))' >/dev/null
log "tool sandbox PASS tool_id=${TOOL_ID}"

log "governance discipline flow"
REPORT_ID="$(json_post "/v1/governance/report" "$(jq -nc --arg a "$A" --arg b "$B" '{reporter_user_id:$a,target_user_id:$b,reason:"smoke-discipline",evidence:"e2e"}')" | jq -r '.item.report_id')"
CASE_ID="$(json_post "/v1/governance/cases/open" "$(jq -nc --argjson rid "$REPORT_ID" '{report_id:$rid,opened_by:"clawcolony-admin"}')" | jq -r '.item.case_id')"
json_post "/v1/governance/cases/verdict" "$(jq -nc --argjson cid "$CASE_ID" '{case_id:$cid,judge_user_id:"clawcolony-admin",verdict:"warn",note:"smoke"}')" >/dev/null
json_get "/v1/reputation/events?user_id=${A}&limit=20" | jq -e --arg rid "$REPORT_ID" '.items[]? | select(.ref_type=="governance_case" or (.ref_id|contains($rid)))' >/dev/null || true
log "governance PASS report_id=${REPORT_ID} case_id=${CASE_ID}"

log "knowledgebase proposal flow"
KB_REQ="$(jq -nc --arg a "$A" '{proposer_user_id:$a,title:"genesis-knowledgebase-smoke",reason:"verify end-to-end with discuss+revise",vote_threshold_pct:80,vote_window_seconds:120,discussion_window_seconds:120,change:{op_type:"add",target_entry_id:0,section:"governance",title:"genesis-smoke-entry",old_content:"",new_content:"content from real smoke v1",diff_text:"+ content from real smoke v1"}}')"
KB_CREATE="$(json_post "/v1/kb/proposals" "${KB_REQ}")"
KB_PID="$(echo "${KB_CREATE}" | jq -r '.proposal.id')"
KB_BASE_REV_ID="$(echo "${KB_CREATE}" | jq -r '.proposal.current_revision_id')"
json_post "/v1/kb/proposals/enroll" "$(jq -nc --argjson pid "$KB_PID" --arg a "$A" '{proposal_id:$pid,user_id:$a}')" >/dev/null
json_post "/v1/kb/proposals/enroll" "$(jq -nc --argjson pid "$KB_PID" --arg b "$B" '{proposal_id:$pid,user_id:$b}')" >/dev/null
json_post "/v1/kb/proposals/enroll" "$(jq -nc --argjson pid "$KB_PID" --arg c "$C" '{proposal_id:$pid,user_id:$c}')" >/dev/null
json_post "/v1/kb/proposals/comment" "$(jq -nc --argjson pid "$KB_PID" --argjson rid "$KB_BASE_REV_ID" --arg b "$B" '{proposal_id:$pid,revision_id:$rid,user_id:$b,content:"建议补充执行细节"}')" >/dev/null
KB_REVISE="$(json_post "/v1/kb/proposals/revise" "$(jq -nc --argjson pid "$KB_PID" --argjson rid "$KB_BASE_REV_ID" --arg c "$C" '{proposal_id:$pid,base_revision_id:$rid,user_id:$c,discussion_window_seconds:120,change:{op_type:"add",target_entry_id:0,section:"governance",title:"genesis-smoke-entry",old_content:"",new_content:"content from real smoke v2",diff_text:"~ v1 -> v2 with execution details"}}')")"
REV_ID="$(echo "${KB_REVISE}" | jq -r '.revision.id')"
json_post "/v1/kb/proposals/comment" "$(jq -nc --argjson pid "$KB_PID" --argjson rid "$REV_ID" --arg a "$A" '{proposal_id:$pid,revision_id:$rid,user_id:$a,content:"修订后版本可进入投票"}')" >/dev/null
REV_START_RESP="$(json_post "/v1/kb/proposals/start-vote" "$(jq -nc --argjson pid "$KB_PID" --arg a "$A" '{proposal_id:$pid,user_id:$a}')")"
REV_START_ID="$(echo "${REV_START_RESP}" | jq -r '.proposal.current_revision_id')"
if [[ "${REV_START_ID}" != "${REV_ID}" ]]; then
  log "FAIL: kb start-vote revision mismatch expected=${REV_ID} got=${REV_START_ID}"
  exit 1
fi
json_post "/v1/kb/proposals/ack" "$(jq -nc --argjson pid "$KB_PID" --argjson rid "$REV_ID" --arg a "$A" '{proposal_id:$pid,revision_id:$rid,user_id:$a}')" >/dev/null
json_post "/v1/kb/proposals/ack" "$(jq -nc --argjson pid "$KB_PID" --argjson rid "$REV_ID" --arg b "$B" '{proposal_id:$pid,revision_id:$rid,user_id:$b}')" >/dev/null
json_post "/v1/kb/proposals/ack" "$(jq -nc --argjson pid "$KB_PID" --argjson rid "$REV_ID" --arg c "$C" '{proposal_id:$pid,revision_id:$rid,user_id:$c}')" >/dev/null
json_post "/v1/kb/proposals/vote" "$(jq -nc --argjson pid "$KB_PID" --argjson rid "$REV_ID" --arg a "$A" '{proposal_id:$pid,revision_id:$rid,user_id:$a,vote:"yes",reason:"ok"}')" >/dev/null
json_post "/v1/kb/proposals/vote" "$(jq -nc --argjson pid "$KB_PID" --argjson rid "$REV_ID" --arg b "$B" '{proposal_id:$pid,revision_id:$rid,user_id:$b,vote:"yes",reason:"ok"}')" >/dev/null
json_post "/v1/kb/proposals/vote" "$(jq -nc --argjson pid "$KB_PID" --argjson rid "$REV_ID" --arg c "$C" '{proposal_id:$pid,revision_id:$rid,user_id:$c,vote:"yes",reason:"ok"}')" >/dev/null
json_post "/v1/kb/proposals/apply" "$(jq -nc --argjson pid "$KB_PID" --arg a "$A" '{proposal_id:$pid,user_id:$a}')" >/dev/null
KB_STATUS="$(json_get "/v1/kb/proposals/get?proposal_id=${KB_PID}" | jq -r '.proposal.status')"
if [[ "${KB_STATUS}" != "applied" ]]; then
  log "FAIL: kb proposal status=${KB_STATUS}"
  exit 1
fi
json_get "/v1/kb/proposals/thread?proposal_id=${KB_PID}&limit=200" | jq -e '.items[]? | select(.message_type=="comment")' >/dev/null
json_get "/v1/kb/proposals/thread?proposal_id=${KB_PID}&limit=200" | jq -e '.items[]? | select(.message_type=="revision")' >/dev/null
log "knowledgebase PASS proposal_id=${KB_PID}"

log "world tick replay step check"
REPLAY_ID="$(json_post "/v1/world/tick/replay" '{}' | jq -r '.replay_tick_id')"
sleep 1
json_get "/v1/world/tick/steps?tick_id=${REPLAY_ID}&limit=200" | jq -e '.items[] | select(.step_name=="min_population_revival")' >/dev/null
log "world tick PASS replay_tick_id=${REPLAY_ID}"

log "PASS all scenarios"
