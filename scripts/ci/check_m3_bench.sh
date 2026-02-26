#!/usr/bin/env bash
set -euo pipefail

cmd_log="$(mktemp -t m3-cmd-bench.XXXXXX.log)"
proxy_log="$(mktemp -t m3-proxy-bench.XXXXXX.log)"

cleanup() {
  rm -f "$cmd_log" "$proxy_log"
}
trap cleanup EXIT

go test ./internal/commandments -run TestEvalLatencySLO -count=1 -v | tee "$cmd_log"
go test ./internal/adapter/proxy -run 'TestProxy(Latency|FirstToken)Gate' -count=1 -v | tee "$proxy_log"

grep -q "m3_bench commandment_eval" "$cmd_log"
grep -q "m3_bench proxy_roundtrip" "$proxy_log"
grep -q "m3_bench proxy_first_token" "$proxy_log"
