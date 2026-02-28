#!/bin/bash
set -euo pipefail

PROJECT="${1:-zns-iscsi-target}"
PASS=0
FAIL=0
TOTAL=0

gate() {
  local num="$1" name="$2"
  shift 2
  TOTAL=$((TOTAL + 1))
  echo -n "Gate $num: $name ... "
  if eval "$@" > /tmp/gate-${num}.log 2>&1; then
    echo "PASS"
    PASS=$((PASS + 1))
  else
    echo "FAIL"
    FAIL=$((FAIL + 1))
    tail -5 /tmp/gate-${num}.log
  fi
}

echo "=== Quality Gates for $PROJECT ==="
echo ""

# Gate 1: Backend Build
gate 1 "Backend Build" "cd $(pwd) && go build ./..."

# Gate 2: Backend Tests
gate 2 "Backend Tests" "cd $(pwd) && go test ./..."

# Gate 3: Frontend Build
gate 3 "Frontend Build" "cd $(pwd)/web && npm run build"

# Gate 4: Frontend Tests
gate 4 "Frontend Tests" "cd $(pwd)/web && npm test -- --run"

# Gate 5: Docker Build
gate 5 "Docker Build" "cd $(pwd) && docker build -f docker/Dockerfile -t zns-iscsi ."

# Gate 6: Docker Run
gate 6 "Docker Run" "docker run --rm -d --name zns-iscsi-test zns-iscsi --config /dev/null 2>/dev/null && sleep 3 && docker stop zns-iscsi-test"

# Gate 7: Health Check
gate 7 "Health Check" "curl -sf http://localhost:8080/api/v1/health | grep -q ok"

# Gate 8: API Zones
gate 8 "API Zones" "curl -sf http://localhost:8080/api/v1/zones | grep -q zones"

# Gate 9: API Stats
gate 9 "API Stats" "curl -sf http://localhost:8080/api/v1/stats | grep -q bytes"

echo ""
echo "=== Results: $PASS/$TOTAL passed, $FAIL failed ==="

if [ "$FAIL" -eq 0 ]; then
  echo "ALL GATES PASSED"
  exit 0
else
  echo "SOME GATES FAILED"
  exit 1
fi
