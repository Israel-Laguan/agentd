#!/bin/sh
# llm-smoke.sh - conformance smoke test for an OpenAI-compatible endpoint.
# Probes: text generation, JSON mode, tool calling.
# Exits non-zero only on hard connectivity failure (DNS/TCP refused).

set -u

usage() {
	cat <<EOF
Usage: $(basename "$0") <base_url> <model> [api_key]

  base_url   OpenAI-compatible base URL (e.g. http://127.0.0.1:8080/v1)
  model      Model name to query
  api_key    Optional bearer token (or set LLM_API_KEY)

Env overrides: LLM_BASE_URL, LLM_MODEL, LLM_API_KEY
EOF
	exit 2
}

BASE_URL="${LLM_BASE_URL:-${1:-}}"
MODEL="${LLM_MODEL:-${2:-}}"
API_KEY="${LLM_API_KEY:-${3:-}}"

[ -n "$BASE_URL" ] || usage
[ -n "$MODEL" ] || usage

if command -v curl >/dev/null 2>&1; then
	CURL_BIN="curl"
elif command -v wget >/dev/null 2>&1; then
	CURL_BIN="wget"
else
	echo "ERROR: neither curl nor wget found" >&2
	exit 2
fi

TMPFILE="${TMPDIR:-/tmp}/llm_smoke_body.$$}"
cleanup_tmp() { rm -f "$TMPFILE"; }
trap cleanup_tmp EXIT INT TERM

do_post() {
	payload="$1"
	: > "$TMPFILE"

	if [ "$CURL_BIN" = "curl" ]; then
		http_code=$(curl -sS -o "$TMPFILE" -w "%{http_code}" \
			-X POST \
			-H "Content-Type: application/json" \
			${API_KEY:+-H "Authorization: Bearer $API_KEY"} \
			--connect-timeout 10 --max-time 30 \
			"$BASE_URL/chat/completions" \
			-d "$payload" 2>/dev/null) || true
		[ -n "$http_code" ] || http_code="000"
	else
		payloadfile="${TMPFILE}.payload"
		printf '%s' "$payload" > "$payloadfile"
		wget -q -O "$TMPFILE" \
			--header="Content-Type: application/json" \
			${API_KEY:+--header="Authorization: Bearer $API_KEY"} \
			--post-file="$payloadfile" \
			--timeout=30 \
			"$BASE_URL/chat/completions" 2>/dev/null || true
		rm -f "$payloadfile"
		if [ -s "$TMPFILE" ]; then
			http_code="200"
		else
			http_code="000"
		fi
	fi

	if [ "$http_code" = "000" ] && [ ! -s "$TMPFILE" ]; then
		echo "HARD_FAIL"
		return
	fi
	echo "$http_code"
}

extract_content() {
	body="$1"
	if command -v jq >/dev/null 2>&1; then
		printf '%s' "$body" | jq -r '.choices[0].message.content // empty'
		return
	fi
	printf '%s' "$body" | sed -n 's/.*"content":"\([^"]*\)".*/\1/p'
}

is_json() {
	content="$1"
	if command -v jq >/dev/null 2>&1; then
		printf '%s' "$content" | jq -e . >/dev/null 2>&1
		return
	fi
	case "$content" in
	'{'*) return 0 ;;
	'['*) return 0 ;;
	*) return 1 ;;
	esac
}

HARD_FAIL=0
TEXT_RESULT=""
JSON_RESULT=""
TOOL_RESULT=""

# 1. Text generation
payload='{"model":"'"${MODEL}"'","messages":[{"role":"user","content":"Say OK"}]}'
status=$(do_post "$payload")
if [ "$status" = "HARD_FAIL" ]; then
	TEXT_RESULT="FAIL (connectivity)"
	HARD_FAIL=1
else
	body=$(cat "$TMPFILE" 2>/dev/null || true)
	content=$(extract_content "$body")
	if [ -n "$content" ]; then
		TEXT_RESULT="PASS"
	else
		TEXT_RESULT="FAIL (empty content)"
	fi
fi

# 2. JSON mode
payload='{"model":"'"${MODEL}"'","messages":[{"role":"user","content":"Return JSON: {"ok": true}"}],"response_format":{"type":"json_object"}}'
status=$(do_post "$payload")
if [ "$status" = "HARD_FAIL" ]; then
	JSON_RESULT="FAIL (connectivity)"
	HARD_FAIL=1
else
	body=$(cat "$TMPFILE" 2>/dev/null || true)
	content=$(extract_content "$body")
	if [ -n "$content" ] && is_json "$content"; then
		JSON_RESULT="PASS"
	elif [ -n "$content" ]; then
		JSON_RESULT="FAIL (not JSON)"
	else
		JSON_RESULT="FAIL (empty content)"
	fi
fi

# 3. Tool calling
payload='{"model":"'"${MODEL}"'","messages":[{"role":"user","content":"What is the weather?"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}}}]}'
status=$(do_post "$payload")
if [ "$status" = "HARD_FAIL" ]; then
	TOOL_RESULT="FAIL (connectivity)"
	HARD_FAIL=1
else
	body=$(cat "$TMPFILE" 2>/dev/null || true)
	if printf '%s' "$body" | grep -q '"tool_calls"'; then
		TOOL_RESULT="SUPPORTED"
	else
		TOOL_RESULT="NOT SUPPORTED"
	fi
fi

# Verdict table
printf "\n%-20s %-15s %s\n" "PROBE" "RESULT" "DETAIL"
printf "%-20s %-15s %s\n" "--------------------" "---------------" "----------------------"
printf "%-20s %-15s %s\n" "text-generation" "$TEXT_RESULT" ""
printf "%-20s %-15s %s\n" "json-mode" "$JSON_RESULT" ""
printf "%-20s %-15s %s\n" "tool-calling" "$TOOL_RESULT" ""

if [ "$HARD_FAIL" -ne 0 ]; then
	echo "" >&2
	echo "ERROR: hard connectivity failure (could not reach $BASE_URL)" >&2
	exit 1
fi

exit 0
