#!/usr/bin/env bash
set -euo pipefail
BASE="${1:-http://localhost:8082/v1}"

echo "==> GET $BASE/health"
curl -sf "$BASE/health" | head -c 200
echo

echo "==> GET $BASE/ready"
curl -sf "$BASE/ready" | head -c 300
echo

echo "==> GET $BASE/bootstrap"
curl -sf "$BASE/bootstrap" | head -c 400
echo

echo "==> POST $BASE/auth/login"
LOGIN=$(curl -sf -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"finance@iag.africa","password":"Finance123!"}')
echo "$LOGIN" | head -c 300
echo

echo "==> GET $BASE/dashboard"
curl -sf "$BASE/dashboard" | head -c 400
echo

echo "==> GET $BASE/invoices"
curl -sf "$BASE/invoices" | head -c 300
echo

echo "==> GET $BASE/banking/accounts"
curl -sf "$BASE/banking/accounts" | head -c 200
echo

echo "==> GET $BASE/assets"
curl -sf "$BASE/assets" | head -c 300
echo

echo "==> GET $BASE/expenses"
curl -sf "$BASE/expenses" | head -c 200
echo

echo "==> GET $BASE/settings"
curl -sf "$BASE/settings" | head -c 200
echo

echo "OK — finance API smoke test passed"
