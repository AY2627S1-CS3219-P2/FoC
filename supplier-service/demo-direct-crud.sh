#!/usr/bin/env bash
# D2 point 3 demo: Supplier Service CRUD, called directly on :8082.
# No gateway, no login, no UI. Needs the local override that publishes
# port 8082 (docker-compose.override.yml); on main the port is closed.
#
# Usage: ./demo-direct-crud.sh
set -uo pipefail

BASE="${SUPPLIER_DIRECT_BASE:-http://localhost:8082}"
PAUSE="${DEMO_PAUSE:-1}"

step() {
  printf '\n\033[1;34m== %s ==\033[0m\n' "$1"
  sleep "$PAUSE"
}

step "1. Service is running and connected to Postgres"
curl -s "$BASE/suppliers" | python3 -c "import json,sys; print(len(json.load(sys.stdin)), 'suppliers in the database')"

# No gateway here, so a 403 comes from supplier-service's RequireAdmin check.
step "2. Create without a role header (expect 403)"
curl -s -o /dev/null -w "HTTP %{http_code} (expect 403)\n" -X POST "$BASE/suppliers" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Direct Demo Stall","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.2966,"longitude":103.7764}'

# X-User-Role is set by hand because there is no gateway to inject it.
step "3. Create as ADMIN"
CREATE_RESP=$(curl -s -X POST "$BASE/suppliers" \
  -H 'Content-Type: application/json' -H 'X-User-Role: ADMIN' \
  -d '{"name":"Direct Demo Stall","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.2966,"longitude":103.7764}')
echo "$CREATE_RESP" | python3 -m json.tool
SID=$(echo "$CREATE_RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
if [ -z "$SID" ]; then
  echo "Create failed, aborting." >&2
  exit 1
fi
echo "Captured id: $SID"

step "4. Read it back"
curl -s "$BASE/suppliers/$SID" | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['name'], ':', d['building'])"

step "5. Update it (mark unavailable)"
curl -s -X PUT "$BASE/suppliers/$SID" -H 'Content-Type: application/json' -H 'X-User-Role: ADMIN' \
  -d '{"name":"Direct Demo Stall","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.2966,"longitude":103.7764,"is_available":false}' \
  | python3 -c "import json,sys; d=json.load(sys.stdin); print('is_available now:', d['is_available'])"

# Soft delete: sets deleted_at, row stays in the table.
step "6. Delete it (expect 204)"
curl -s -o /dev/null -w "HTTP %{http_code} (expect 204)\n" -X DELETE "$BASE/suppliers/$SID" -H 'X-User-Role: ADMIN'

step "7. Get after delete (expect 404)"
curl -s -o /dev/null -w "HTTP %{http_code} (expect 404)\n" "$BASE/suppliers/$SID"

echo
echo "Done."
