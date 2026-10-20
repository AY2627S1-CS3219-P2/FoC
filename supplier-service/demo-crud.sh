#!/usr/bin/env bash
# D2 point 3 demo: Supplier Service CRUD plus list, filter and search, called
# directly on :8082. No gateway, no login, no UI. Needs jq and the local
# override that publishes port 8082 (docker-compose.override.yml).
#
# Usage: ./demo-crud.sh
set -uo pipefail

BASE="${SUPPLIER_API_BASE:-http://localhost:8082}"
PAUSE="${DEMO_PAUSE:-1}"  # seconds between steps; set DEMO_PAUSE=0 to skip waiting

step() {
  printf '\n\033[1;34m== %s ==\033[0m\n' "$1"
  sleep "$PAUSE"
}

step "0. Health check"
curl -s "$BASE/health"; echo

step "1. List all suppliers (public, seeded data)"
curl -s "$BASE/suppliers" | jq

step "2. Filter by category"
curl -s "$BASE/suppliers?category=cafe" | jq

step "3. Search by name"
curl -s "$BASE/suppliers?q=arise" | jq

step "4. Create without a role -> expect 403"
curl -s -o /dev/null -w 'HTTP %{http_code}\n' -X POST "$BASE/suppliers" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Demo Cafe","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.2966,"longitude":103.7764}'

step "5. Create as ADMIN -> expect 201"
CREATE_RESP=$(curl -s -X POST "$BASE/suppliers" \
  -H 'Content-Type: application/json' -H 'X-User-Role: ADMIN' \
  -d '{"name":"Demo Cafe","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.2966,"longitude":103.7764}')
echo "$CREATE_RESP" | jq
ID=$(echo "$CREATE_RESP" | jq -r '.id')
if [ -z "$ID" ] || [ "$ID" = "null" ]; then
  echo "Create failed, aborting rest of the demo." >&2
  exit 1
fi
echo "Captured id: $ID"

step "6. Create duplicate (same name+building+location) -> expect 409"
curl -s -o /dev/null -w 'HTTP %{http_code}\n' -X POST "$BASE/suppliers" \
  -H 'Content-Type: application/json' -H 'X-User-Role: ADMIN' \
  -d '{"name":"Demo Cafe","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.2966,"longitude":103.7764}'

step "7. Update: mark unavailable -> expect 200, is_available:false"
curl -s -X PUT "$BASE/suppliers/$ID" \
  -H 'Content-Type: application/json' -H 'X-User-Role: ADMIN' \
  -d '{"name":"Demo Cafe","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.2966,"longitude":103.7764,"is_available":false}' | jq

step "8. Delete as ADMIN -> expect 204"
curl -s -o /dev/null -w 'HTTP %{http_code}\n' -X DELETE "$BASE/suppliers/$ID" -H 'X-User-Role: ADMIN'

step "9. Get after delete -> expect 404 (soft-deleted: row kept, hidden from the API)"
curl -s -o /dev/null -w 'HTTP %{http_code}\n' "$BASE/suppliers/$ID"

step "10. Validation error -> expect 400"
curl -s -X POST "$BASE/suppliers" -H 'Content-Type: application/json' -H 'X-User-Role: ADMIN' -d '{}' | jq

echo
echo "Done."
