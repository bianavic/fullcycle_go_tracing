#!/usr/bin/env bash
#
# End-to-end check of the whole ecosystem through docker compose.
#
#   make e2e                      # or: ./scripts/e2e.sh
#
# Needs: docker compose, curl, jq, and a .env with a real WEATHER_API_KEY. It calls
# the real ViaCEP and WeatherAPI, so it needs internet access.
#
# Environment:
#   E2E_SKIP_UP=1   reuse a stack that is already running (and leave it running)
#   E2E_KEEP=1      leave the stack running after the checks
#   SERVICE_A_URL   default http://localhost:8080
#   ZIPKIN_URL      default http://localhost:9411
#   ZIPKIN_TIMEOUT  seconds to wait for the trace to reach Zipkin (default 40)
set -uo pipefail

cd "$(dirname "$0")/.."

A_URL="${SERVICE_A_URL:-http://localhost:8080}"
ZIPKIN="${ZIPKIN_URL:-http://localhost:9411}"
ZIPKIN_TIMEOUT="${ZIPKIN_TIMEOUT:-40}"
VALID_CEP="29902555"   # Linhares/ES
MISSING_CEP="99999999" # well formed, does not exist

for tool in docker curl jq; do
  command -v "${tool}" >/dev/null || { echo "missing required tool: ${tool}" >&2; exit 2; }
done

work="$(mktemp -d)"
passed=0
failed=0

cleanup() {
  rm -rf "${work}"
  if [[ -z "${E2E_SKIP_UP:-}" && -z "${E2E_KEEP:-}" ]]; then
    echo "==> stopping the stack"
    docker compose down >/dev/null 2>&1
  fi
}
trap cleanup EXIT

pass() { passed=$((passed + 1)); printf '  \033[32mPASS\033[0m %s\n' "$1"; }
fail() { failed=$((failed + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; [[ -n "${2:-}" ]] && printf '       %s\n' "$2"; }

# expect_json <description> <jq -e filter> <file>
expect_json() {
  if jq -e "$2" "$3" >/dev/null 2>&1; then pass "$1"; else fail "$1" "got: $(head -c 300 "$3")"; fi
}

# call <method> <path> <body> -> sets $status; response body in ${work}/body, headers in ${work}/headers
call() {
  status="$(curl -sS -o "${work}/body" -D "${work}/headers" -w '%{http_code}' -X "$1" \
    -H 'Content-Type: application/json' "${@:4}" ${3:+-d "$3"} "${A_URL}$2")"
}

expect_status() { [[ "${status}" == "$2" ]] && pass "$1 → HTTP $2" || fail "$1 → expected HTTP $2" "got HTTP ${status}: $(head -c 200 "${work}/body")"; }

if [[ -z "${E2E_SKIP_UP:-}" ]]; then
  echo "==> starting the stack (docker compose up --build --wait)"
  docker compose up -d --build --wait --wait-timeout 180 || { echo "stack failed to start" >&2; exit 1; }
fi

random_hex() { od -An -N"$1" -tx1 /dev/urandom | tr -d ' \n'; }
trace_id="$(random_hex 16)"
parent_id="$(random_hex 8)"

echo "==> Service A: success"
call POST /weather "{\"cep\":\"${VALID_CEP}\"}" -H "traceparent: 00-${trace_id}-${parent_id}-01"
expect_status "valid CEP ${VALID_CEP}" 200
expect_json "body has city, temp_C, temp_F and temp_K with the right types" \
  'keys == ["city","temp_C","temp_F","temp_K"] and (.city|type=="string" and length>0) and ([.temp_C,.temp_F,.temp_K]|all(type=="number"))' "${work}/body"
expect_json "temp_F = temp_C × 1.8 + 32 (±0.1)" '((.temp_C*1.8+32) - .temp_F | fabs) <= 0.1' "${work}/body"
expect_json "temp_K = temp_C + 273 (±0.1)" '((.temp_C+273) - .temp_K | fabs) <= 0.1' "${work}/body"
grep -qi '^content-type: application/json' "${work}/headers" && pass "Content-Type is application/json" || fail "Content-Type is application/json"

echo "==> Service A: errors"
call POST /weather "{\"cep\":\"${MISSING_CEP}\"}"
expect_status "well-formed but unknown CEP" 404
expect_json "404 body is {message: can not find zipcode}" '. == {"message":"can not find zipcode"}' "${work}/body"

for body in '{"cep":"123"}' '{"cep":"29902-555"}' '{"cep":"ABCDEFGH"}' '{"cep":29902555}' '{"cep":null}' '{}' 'not json' "{\"cep\":\"${VALID_CEP}\",\"x\":1}"; do
  call POST /weather "${body}"
  expect_status "invalid input ${body}" 422
  expect_json "  body is {message: invalid zipcode}" '. == {"message":"invalid zipcode"}' "${work}/body"
done

# Bodies over 1 KB are rejected before parsing.
call POST /weather "{\"cep\":\"$(head -c 2000 /dev/zero | tr '\0' 1)\"}"
expect_status "body larger than 1 KB" 413
expect_json "  body is {message: request body too large}" '. == {"message":"request body too large"}' "${work}/body"

call GET /weather ""
expect_status "GET /weather" 405

echo "==> Edge request id"
call POST /weather "{\"cep\":\"${MISSING_CEP}\"}" -H 'X-Request-Id: client-chosen-id'
rid="$(awk 'tolower($1)=="x-request-id:" {print $2}' "${work}/headers" | tr -d '\r')"
[[ -n "${rid}" && "${rid}" != "client-chosen-id" ]] && pass "client X-Request-Id is ignored; server issued ${rid}" || fail "client X-Request-Id is ignored" "got '${rid}'"

echo "==> Zipkin: distributed trace ${trace_id}"
[[ "$(curl -s -o /dev/null -w '%{http_code}' "${ZIPKIN}/zipkin/")" == "200" ]] && pass "Zipkin UI reachable at ${ZIPKIN}/zipkin/" || fail "Zipkin UI reachable at ${ZIPKIN}/zipkin/"

# Spans reach Zipkin after the SDK batch delay (5s) plus the collector batch (1s).
deadline=$((SECONDS + ZIPKIN_TIMEOUT))
found=""
while (( SECONDS < deadline )); do
  if curl -sf "${ZIPKIN}/api/v2/trace/${trace_id}" -o "${work}/trace.json" \
     && jq -e '
          # Each service exports its own batch, so wait until all five spans have arrived.
          [.[] | "\(.localEndpoint.serviceName)/\(.name)"] as $got |
          ["service-a/post /weather", "service-a/http post", "service-b/post /weather",
           "service-b/lookup-cep", "service-b/lookup-temperature"] | all(. as $w | $got | index($w) != null)' \
          "${work}/trace.json" >/dev/null 2>&1; then
    found=1; break
  fi
  sleep 2
done

if [[ -z "${found}" ]]; then
  fail "trace ${trace_id} reached Zipkin within ${ZIPKIN_TIMEOUT}s"
else
  pass "trace ${trace_id} reached Zipkin (the traceparent sent to service-a was continued)"
  jq -e '
    def svc: .localEndpoint.serviceName;
    def span($s; $n): map(select(svc == $s and .name == $n))[0];
    span("service-a"; "post /weather") as $aServer |
    span("service-a"; "http post")      as $aClient |
    span("service-b"; "post /weather")  as $bServer |
    span("service-b"; "lookup-cep")     as $cep |
    span("service-b"; "lookup-temperature") as $temp |
    [$aServer, $aClient, $bServer, $cep, $temp] | all(. != null)' "${work}/trace.json" >/dev/null \
    && pass "spans present: Request → Service A → Service B, plus lookup-cep and lookup-temperature" \
    || fail "spans present: service-a server/client, service-b server, lookup-cep, lookup-temperature" \
            "got: $(jq -c 'map({s: .localEndpoint.serviceName, n: .name})' "${work}/trace.json")"
  jq -e --arg parent "${parent_id}" '
    def svc: .localEndpoint.serviceName;
    def span($s; $n): map(select(svc == $s and .name == $n))[0];
    span("service-a"; "post /weather") as $aServer |
    span("service-a"; "http post")      as $aClient |
    span("service-b"; "post /weather")  as $bServer |
    $aServer.parentId == $parent and
    $aClient.parentId == $aServer.id and
    $bServer.parentId == $aClient.id and
    span("service-b"; "lookup-cep").parentId == $bServer.id and
    span("service-b"; "lookup-temperature").parentId == $bServer.id' "${work}/trace.json" >/dev/null \
    && pass "parent/child links: caller → A server → A client → B server → lookup-cep / lookup-temperature" \
    || fail "parent/child links form one connected tree" "got: $(jq -c 'map({s: .localEndpoint.serviceName, n: .name, id: .id, p: .parentId})' "${work}/trace.json")"
  jq -e 'map(.duration) | all(. != null and . > 0)' "${work}/trace.json" >/dev/null \
    && pass "every span has a measured duration" || fail "every span has a measured duration"
fi

echo
echo "==> ${passed} passed, ${failed} failed"
[[ "${failed}" -eq 0 ]]
