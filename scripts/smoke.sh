#!/usr/bin/env bash
set -euo pipefail

api_base="${SPEAKUP_API_BASE:-http://127.0.0.1:8080/api/v1}"
phone="13$(date +%s)"

auth=$(curl --fail --silent --show-error \
  -X POST "$api_base/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"phone\":\"$phone\",\"password\":\"speakup123\",\"nickname\":\"CI Learner\"}")
token=$(jq -r '.data.access_token' <<<"$auth")

scenes=$(curl --fail --silent --show-error "$api_base/scenes" \
  -H "Authorization: Bearer $token")
if [[ $(jq '.data.total' <<<"$scenes") -lt 2 ]]; then
  echo "expected at least two scenes"
  exit 1
fi

session=$(curl --fail --silent --show-error \
  -X POST "$api_base/sessions" \
  -H "Authorization: Bearer $token" \
  -H 'Content-Type: application/json' \
  -d '{"scene_id":"restaurant-order"}')
export SPEAKUP_SMOKE_SESSION_ID
export SPEAKUP_SMOKE_WS_TOKEN
SPEAKUP_SMOKE_SESSION_ID=$(jq -r '.data.session_id' <<<"$session")
SPEAKUP_SMOKE_WS_TOKEN=$(jq -r '.data.ws_token' <<<"$session")

(
  cd ai
  uv run python - <<'PY'
import json
import os

from websockets.sync.client import connect

url = "ws://127.0.0.1:8080/ws/conversation?session_id={}&token={}".format(
    os.environ["SPEAKUP_SMOKE_SESSION_ID"],
    os.environ["SPEAKUP_SMOKE_WS_TOKEN"],
)
seen_reply = False
with connect(url, open_timeout=5) as websocket:
    websocket.send(
        json.dumps(
            {
                "type": "text",
                "payload": {
                    "turn_seq": 1,
                    "text": "I would like the tomato soup, please.",
                },
            }
        )
    )
    while True:
        frame = json.loads(websocket.recv(timeout=10))
        seen_reply = seen_reply or frame["type"] == "reply_delta"
        if frame["type"] == "turn_end":
            break
if not seen_reply:
    raise SystemExit("reply_delta was not received")
PY
)

detail=$(curl --fail --silent --show-error \
  "$api_base/sessions/$SPEAKUP_SMOKE_SESSION_ID" \
  -H "Authorization: Bearer $token")
if [[ $(jq '.data.turns | length' <<<"$detail") -ne 1 ]]; then
  echo "expected one persisted turn"
  exit 1
fi

curl --fail --silent --show-error \
  -X POST "$api_base/sessions/$SPEAKUP_SMOKE_SESSION_ID/end" \
  -H "Authorization: Bearer $token" >/dev/null

evaluation=''
for _ in $(seq 1 20); do
  evaluation=$(curl --fail --silent --show-error \
    "$api_base/evaluations?session_id=$SPEAKUP_SMOKE_SESSION_ID" \
    -H "Authorization: Bearer $token")
  if [[ $(jq -r '.data[0].status // empty' <<<"$evaluation") == done ]]; then
    break
  fi
  sleep 1
done

if [[ $(jq -r '.data[0].status // empty' <<<"$evaluation") != done ]]; then
  echo "evaluation did not complete"
  exit 1
fi

jq '{session_id: .data[0].session_id, status: .data[0].status, overall_score: .data[0].overall_score}' <<<"$evaluation"
