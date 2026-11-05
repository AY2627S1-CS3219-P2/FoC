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

ALICE_USER="alice_$(date +%s | tail -c 5)"
ALICE_EMAIL="${ALICE_USER}@u.nus.edu"

BOB_USER="bob_$(date +%s | tail -c 5)"
BOB_EMAIL="${BOB_USER}@u.nus.edu"

CHARLIE_USER="charlie_$(date +%s | tail -c 5)"
CHARLIE_EMAIL="${CHARLIE_USER}@u.nus.edu"

echo "================================================================================"
echo "🚀 1. Starting Docker Compose Services..."
echo "================================================================================"

docker compose up -d

echo "⏳ Waiting for api-gateway (port 8080) to become healthy..."
until curl -s -f "${GATEWAY_URL}/api/suppliers" > /dev/null 2>&1; do
  sleep 2
done
echo "✅ Gateway and backend services are ready!"

# Helper to extract OTP from user-service logs
get_otp() {
  local email="$1"
  local attempts=0
  local otp=""
  while [ $attempts -lt 20 ]; do
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

  echo "👤 Registering ${user} (${email})..."
  curl -s -f -X POST "${GATEWAY_URL}/auth/register" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"${email}\",\"username\":\"${user}\",\"password\":\"${PASSWORD}\"}" > /dev/null

  local otp
  otp=$(get_otp "$email")

  curl -s -f -X POST "${GATEWAY_URL}/auth/verify-otp" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"${email}\",\"otp\":\"${otp}\"}" > /dev/null

  local token
  token=$(curl -s -f -X POST "${GATEWAY_URL}/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"identifier\":\"${user}\",\"password\":\"${PASSWORD}\"}" | grep -o '"token":"[^"]*' | cut -d'"' -f4)
  
  echo "$token"
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

echo ""
echo "================================================================================"
echo "📦 3. Creating Errand by Requester Alice..."
echo "================================================================================"

SUPPLIER_ID=$(curl -s -f "${GATEWAY_URL}/api/suppliers" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4)

ERRAND_RESP=$(curl -s -f -X POST "${GATEWAY_URL}/api/orders" \
  -H "Authorization: Bearer ${ALICE_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{
    \"supplierId\": \"${SUPPLIER_ID}\",
    \"itemDescription\": \"Iced Lemon Tea from The Deck\",
    \"dropoffLocation\": \"CLB Level 1 Bench\",
    \"fee\": 5
  }")

ERRAND_ID=$(echo "$ERRAND_RESP" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4)
echo "✅ Errand created with ID: ${ERRAND_ID}"

echo ""
echo "================================================================================"
echo "🤝 4. Courier Bob Accepts the Errand & Sends a Message..."
echo "================================================================================"

curl -s -f -X POST "${GATEWAY_URL}/api/orders/${ERRAND_ID}/accept" \
  -H "Authorization: Bearer ${BOB_TOKEN}" > /dev/null
echo "   Bob accepted the errand."

sleep 2 # Allow RabbitMQ event to create chat conversation

BOB_CONV_ID=$(curl -s -f "${GATEWAY_URL}/api/chat/conversations?errandId=${ERRAND_ID}" \
  -H "Authorization: Bearer ${BOB_TOKEN}" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4)

curl -s -f -X POST "${GATEWAY_URL}/api/chat/conversations/${BOB_CONV_ID}/messages" \
  -H "Authorization: Bearer ${BOB_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"messageType\":\"TEXT\",\"textContent\":\"Hi Alice! I accepted your errand, on my way to The Deck!\"}" > /dev/null

curl -s -f -X POST "${GATEWAY_URL}/api/chat/conversations/${BOB_CONV_ID}/messages" \
  -H "Authorization: Bearer ${ALICE_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"messageType\":\"TEXT\",\"textContent\":\"Thanks Bob! Less ice please if possible :)\"}" > /dev/null

echo "✅ Messages exchanged between Alice and Bob."

echo ""
echo "================================================================================"
echo "🚪 5. Courier Bob Withdraws from the Errand..."
echo "================================================================================"

curl -s -f -X POST "${GATEWAY_URL}/api/orders/${ERRAND_ID}/withdraw" \
  -H "Authorization: Bearer ${BOB_TOKEN}" > /dev/null
echo "   Bob withdrew from the errand."

sleep 2 # Allow RabbitMQ event to freeze Bob's chat session

echo ""
echo "================================================================================"
echo "🚴 6. Courier Charlie Accepts the Errand & Sends a Message..."
echo "================================================================================"

curl -s -f -X POST "${GATEWAY_URL}/api/orders/${ERRAND_ID}/accept" \
  -H "Authorization: Bearer ${CHARLIE_TOKEN}" > /dev/null
echo "   Charlie accepted the errand."

sleep 2 # Allow RabbitMQ event to create new chat session for Charlie

CHARLIE_CONV_ID=$(curl -s -f "${GATEWAY_URL}/api/chat/conversations?errandId=${ERRAND_ID}" \
  -H "Authorization: Bearer ${CHARLIE_TOKEN}" | grep -o '"id":"[^"]*' | head -n 1 | cut -d'"' -f4)

curl -s -f -X POST "${GATEWAY_URL}/api/chat/conversations/${CHARLIE_CONV_ID}/messages" \
  -H "Authorization: Bearer ${CHARLIE_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"messageType\":\"TEXT\",\"textContent\":\"Hello Alice, Charlie here taking over your errand!\"}" > /dev/null

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
