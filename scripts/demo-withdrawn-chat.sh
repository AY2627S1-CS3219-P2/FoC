#!/usr/bin/env bash
# ==============================================================================
# demo-withdrawn-chat.sh
#
# Starts up the FoC Docker Compose stack, registers seed users (Requester Alice,
# Courier Bob, Courier Charlie), creates an errand, simulates courier withdrawal,
# and brings in a new courier so you can observe the multi-courier chat dropdown
# and archived conversation behavior.
# ==============================================================================

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

GATEWAY_URL="http://localhost:8080"
PASSWORD="Password123"

# Username must be strictly alphanumeric (no underscores)
SUFFIX="$(date +%s | tail -c 5)"
ALICE_USER="alice${SUFFIX}"
ALICE_EMAIL="${ALICE_USER}@u.nus.edu"

BOB_USER="bob${SUFFIX}"
BOB_EMAIL="${BOB_USER}@u.nus.edu"

CHARLIE_USER="charlie${SUFFIX}"
CHARLIE_EMAIL="${CHARLIE_USER}@u.nus.edu"

echo "================================================================================"
echo "🚀 1. Starting Docker Compose Services..."
echo "================================================================================"

docker compose up -d

echo "⏳ Waiting for api-gateway (port 8080) to become healthy..."
until curl -s -f "${GATEWAY_URL}/healthz" > /dev/null 2>&1; do
  sleep 2
done
echo "✅ Gateway and backend services are ready!"

# Helper to extract OTP from user-service logs
get_otp() {
  local email="$1"
  local attempts=0
  local otp=""
  while [ $attempts -lt 25 ]; do
    otp=$(docker compose logs --no-log-prefix user-service 2>/dev/null | grep "\"email\":\"${email}\"" | grep -o '"otp":"[0-9]*"' | tail -n 1 | cut -d'"' -f4 || true)
    if [ -n "$otp" ]; then
      echo "$otp"
      return 0
    fi
    sleep 1
    attempts=$((attempts + 1))
  done
  echo "Error: OTP for $email not found in logs." >&2
  return 1
}

# Helper to register and verify an account, returning its bearer access token
register_and_login() {
  local user="$1"
  local email="$2"

  echo "👤 Registering ${user} (${email})..." >&2
  curl -s -S -f -X POST "${GATEWAY_URL}/auth/register" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"${email}\",\"username\":\"${user}\",\"password\":\"${PASSWORD}\"}" > /dev/null

  local otp
  otp=$(get_otp "$email")

  curl -s -S -f -X POST "${GATEWAY_URL}/auth/verify-otp" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"${email}\",\"otp\":\"${otp}\"}" > /dev/null

  local token
  token=$(curl -s -S -f -X POST "${GATEWAY_URL}/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"identifier\":\"${user}\",\"password\":\"${PASSWORD}\"}" | grep -o '"accessToken":"[^"]*' | cut -d'"' -f4)
  
  echo "$token"
}

# Generate UUID v4
random_uuid() {
  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr '[:upper:]' '[:lower:]'
  else
    python3 -c 'import uuid; print(uuid.uuid4())'
  fi
}

echo ""
echo "================================================================================"
echo "👥 2. Creating Demo Users..."
echo "================================================================================"

ALICE_TOKEN=$(register_and_login "$ALICE_USER" "$ALICE_EMAIL")
echo "   Alice Token acquired."

BOB_TOKEN=$(register_and_login "$BOB_USER" "$BOB_EMAIL")
echo "   Bob Token acquired."

CHARLIE_TOKEN=$(register_and_login "$CHARLIE_USER" "$CHARLIE_EMAIL")
echo "   Charlie Token acquired."

# Wait for credit account to be initialized via user.registered event
echo "⏳ Waiting for Alice's starting credit allocation..."
until curl -s -f "${GATEWAY_URL}/api/credits/accounts/me" -H "Authorization: Bearer ${ALICE_TOKEN}" > /dev/null 2>&1; do
  sleep 1
done
echo "✅ Alice's credit account ready!"

echo ""
echo "================================================================================"
echo "📦 3. Creating Errand by Requester Alice..."
echo "================================================================================"

SUPPLIER_ID=$(curl -s -S -f "${GATEWAY_URL}/api/suppliers" -H "Authorization: Bearer ${ALICE_TOKEN}" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4)
LOCATION=$(curl -s -S -f "${GATEWAY_URL}/api/orders/locations" -H "Authorization: Bearer ${ALICE_TOKEN}" | grep -o '"name":"[^"]*' | head -n 1 | cut -d'"' -f4)
REQ_ID=$(random_uuid)

ERRAND_RESP=$(curl -s -S -f -X POST "${GATEWAY_URL}/api/orders" \
  -H "Authorization: Bearer ${ALICE_TOKEN}" \
  -H "X-Request-Id: ${REQ_ID}" \
  -H "Content-Type: application/json" \
  -d "{
    \"supplierId\": \"${SUPPLIER_ID}\",
    \"itemDescription\": \"Iced Lemon Tea from The Deck\",
    \"deliveryLocation\": \"${LOCATION}\",
    \"reward\": 5
  }")

ERRAND_ID=$(echo "$ERRAND_RESP" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4)
VERSION=$(echo "$ERRAND_RESP" | grep -o '"version":[0-9]*' | head -n 1 | cut -d':' -f2)
echo "✅ Errand created with ID: ${ERRAND_ID} (version: ${VERSION})"

echo ""
echo "================================================================================"
echo "🤝 4. Courier Bob Accepts the Errand & Sends a Message..."
echo "================================================================================"

ACCEPT_RESP=$(curl -s -S -f -X POST "${GATEWAY_URL}/api/orders/${ERRAND_ID}/accept" \
  -H "Authorization: Bearer ${BOB_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"version\": ${VERSION}}")
echo "   Bob accepted the errand."

VERSION=$(echo "$ACCEPT_RESP" | grep -o '"version":[0-9]*' | head -n 1 | cut -d':' -f2)

sleep 2 # Allow RabbitMQ event to create chat conversation

BOB_CONV_ID=$(curl -s -S -f "${GATEWAY_URL}/api/chat/conversations?errandId=${ERRAND_ID}" \
  -H "Authorization: Bearer ${BOB_TOKEN}" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4)

curl -s -S -f -X POST "${GATEWAY_URL}/api/chat/conversations/${BOB_CONV_ID}/messages" \
  -H "Authorization: Bearer ${BOB_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"client_message_id\":\"$(random_uuid)\",\"text\":\"Hi Alice! I accepted your errand, on my way to The Deck!\"}" > /dev/null

curl -s -S -f -X POST "${GATEWAY_URL}/api/chat/conversations/${BOB_CONV_ID}/messages" \
  -H "Authorization: Bearer ${ALICE_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"client_message_id\":\"$(random_uuid)\",\"text\":\"Thanks Bob! Less ice please if possible :)\"}" > /dev/null

echo "✅ Messages exchanged between Alice and Bob."

echo ""
echo "================================================================================"
echo "🚪 5. Courier Bob Withdraws from the Errand..."
echo "================================================================================"

WITHDRAW_RESP=$(curl -s -S -f -X POST "${GATEWAY_URL}/api/orders/${ERRAND_ID}/withdraw" \
  -H "Authorization: Bearer ${BOB_TOKEN}")
echo "   Bob withdrew from the errand."

VERSION=$(echo "$WITHDRAW_RESP" | grep -o '"version":[0-9]*' | head -n 1 | cut -d':' -f2)

sleep 2 # Allow RabbitMQ event to freeze Bob's chat session

echo ""
echo "================================================================================"
echo "🚴 6. Courier Charlie Accepts the Errand & Sends a Message..."
echo "================================================================================"

curl -s -S -f -X POST "${GATEWAY_URL}/api/orders/${ERRAND_ID}/accept" \
  -H "Authorization: Bearer ${CHARLIE_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"version\": ${VERSION}}" > /dev/null
echo "   Charlie accepted the errand."

sleep 2 # Allow RabbitMQ event to create new chat session for Charlie

CHARLIE_CONV_ID=$(curl -s -S -f "${GATEWAY_URL}/api/chat/conversations?errandId=${ERRAND_ID}" \
  -H "Authorization: Bearer ${CHARLIE_TOKEN}" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4)

curl -s -S -f -X POST "${GATEWAY_URL}/api/chat/conversations/${CHARLIE_CONV_ID}/messages" \
  -H "Authorization: Bearer ${CHARLIE_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"client_message_id\":\"$(random_uuid)\",\"text\":\"Hello Alice, Charlie here taking over your errand!\"}" > /dev/null

echo "✅ Errand reassigned and message sent by Charlie."

echo ""
echo "================================================================================"
echo "🎉 DEMO SETUP READY! HOW TO OBSERVE:"
echo "================================================================================"
echo ""
echo "1. Start the frontend if not already running:"
echo "   $ cd frontend && npm run dev"
echo ""
echo "2. Open your browser at http://localhost:3001 and sign in as:"
echo "   👤 Requester Alice:"
echo "      Username: ${ALICE_USER}"
echo "      Password: ${PASSWORD}"
echo ""
echo "3. Go to 'My Errands' -> Click on the errand (${ERRAND_ID})."
echo "4. Notice the chat header dropdown:"
echo "   ┌────────────────────────────────────────────────────────┐"
echo "   │  Chat Session: [ Courier 2 (Current) ▾ ]              │"
echo "   └────────────────────────────────────────────────────────┘"
echo "   • Click the dropdown to switch between:"
echo "     - Courier 2 (Current)   [Active]    -> Chatting with Charlie"
echo "     - Courier 1 (Withdrawn) [Withdrawn] -> Archived chat with Bob"
echo ""
echo "5. You can also sign in as Charlie (Username: ${CHARLIE_USER}, Password: ${PASSWORD})"
echo "   to verify that Charlie CANNOT see the dropdown or Bob's past messages."
echo "================================================================================"
