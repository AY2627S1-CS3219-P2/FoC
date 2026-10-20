#!/usr/bin/env bash
# D2 point 4 demo: end-to-end integration.
# Path: client -> API Gateway (verifies JWT, strips client identity headers,
# injects X-User-Id / X-User-Role) -> Supplier Service -> Postgres.
# Writes are also checked directly in the database.
#
# Prerequisites:
#   - Full stack running (docker compose up, see root AGENTS.md).
#   - Test accounts from root AGENTS.md section 11:
#     student2 / Student123 and admindemo / AdminDemo123.
#
# Usage: ./demo-e2e-integration.sh
#   GATEWAY_BASE   default http://localhost:8080 (set to http://localhost:8090
#                  if the gateway is published there).
#   DB_CONTAINER   default foc-main-supplier-db-1.
set -uo pipefail

GATEWAY="${GATEWAY_BASE:-http://localhost:8080}"
DB_CONTAINER="${DB_CONTAINER:-foc-main-supplier-db-1}"
PAUSE="${DEMO_PAUSE:-1}"

step() {
  printf '\n\033[1;34m== %s ==\033[0m\n' "$1"
  sleep "$PAUSE"
}

json_field() {
  python3 -c "import json,sys; print(json.load(sys.stdin).get('$1',''))"
}

step "0. Gateway health check"
curl -s "$GATEWAY/healthz"; echo

step "1. Log in as STUDENT (student2) via the gateway"
STUDENT_LOGIN=$(curl -s -X POST "$GATEWAY/auth/login" -H 'Content-Type: application/json' \
  -d '{"identifier":"student2","password":"Student123"}')
STUDENT_TOKEN=$(echo "$STUDENT_LOGIN" | json_field accessToken)
if [ -z "$STUDENT_TOKEN" ]; then
  echo "Could not log in as student2. See root AGENTS.md §11 to register it first." >&2
  exit 1
fi
echo "Logged in. Token role: STUDENT."

step "2. STUDENT lists suppliers"
curl -s "$GATEWAY/api/suppliers" | python3 -c "import json,sys; d=json.load(sys.stdin); print(f'{len(d)} suppliers returned')"

step "3. STUDENT tries to create a supplier (expect 403)"
curl -s -o /dev/null -w "HTTP %{http_code} (expect 403)\n" -X POST "$GATEWAY/api/suppliers" \
  -H "Authorization: Bearer $STUDENT_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"E2E Demo Stall","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.2966,"longitude":103.7764}'

step "4. Log in as ADMIN (admindemo) via the gateway"
ADMIN_LOGIN=$(curl -s -X POST "$GATEWAY/auth/login" -H 'Content-Type: application/json' \
  -d '{"identifier":"admindemo","password":"AdminDemo123"}')
ADMIN_TOKEN=$(echo "$ADMIN_LOGIN" | json_field accessToken)
if [ -z "$ADMIN_TOKEN" ]; then
  echo "Could not log in as admindemo. See root AGENTS.md §11 to bootstrap it first." >&2
  exit 1
fi
echo "Logged in. Token role: ADMIN."

step "5. ADMIN creates a supplier"
CREATE_RESP=$(curl -s -X POST "$GATEWAY/api/suppliers" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"E2E Demo Stall","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.2966,"longitude":103.7764}')
echo "$CREATE_RESP" | python3 -m json.tool
SUPPLIER_ID=$(echo "$CREATE_RESP" | json_field id)
if [ -z "$SUPPLIER_ID" ]; then
  echo "Create failed, aborting rest of the demo." >&2
  exit 1
fi

step "6. Check the new row in Postgres"
docker exec "$DB_CONTAINER" psql -U postgres -d supplier -c \
  "SELECT id, name, is_available FROM suppliers WHERE id = '$SUPPLIER_ID';"

step "7. STUDENT reads the new supplier"
curl -s "$GATEWAY/api/suppliers/$SUPPLIER_ID" -H "Authorization: Bearer $STUDENT_TOKEN" | python3 -m json.tool

step "8. ADMIN updates the supplier"
curl -s -X PUT "$GATEWAY/api/suppliers/$SUPPLIER_ID" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"E2E Demo Stall","type":"cafe","building":"COM1","location_description":"Level 2","latitude":1.2966,"longitude":103.7764,"is_available":false}' \
  | python3 -m json.tool

step "9. STUDENT tries to delete it (expect 403)"
curl -s -o /dev/null -w "HTTP %{http_code} (expect 403)\n" -X DELETE "$GATEWAY/api/suppliers/$SUPPLIER_ID" \
  -H "Authorization: Bearer $STUDENT_TOKEN"

step "10. STUDENT sends a forged X-User-Role: ADMIN header (expect 403)"
curl -s -o /dev/null -w "HTTP %{http_code} (expect 403)\n" \
  -X DELETE "$GATEWAY/api/suppliers/$SUPPLIER_ID" \
  -H "Authorization: Bearer $STUDENT_TOKEN" -H "X-User-Role: ADMIN"

step "11. ADMIN deletes it (soft delete, expect 204)"
curl -s -o /dev/null -w "HTTP %{http_code} (expect 204)\n" -X DELETE "$GATEWAY/api/suppliers/$SUPPLIER_ID" \
  -H "Authorization: Bearer $ADMIN_TOKEN"

step "12. Check Postgres: row kept with deleted_at set; API returns 404"
docker exec "$DB_CONTAINER" psql -U postgres -d supplier -c \
  "SELECT id, name, is_available, deleted_at IS NOT NULL AS soft_deleted FROM suppliers WHERE id = '$SUPPLIER_ID';"
curl -s -o /dev/null -w "GET by STUDENT after delete: HTTP %{http_code} (expect 404)\n" \
  "$GATEWAY/api/suppliers/$SUPPLIER_ID" -H "Authorization: Bearer $STUDENT_TOKEN"

echo
echo "Done."
