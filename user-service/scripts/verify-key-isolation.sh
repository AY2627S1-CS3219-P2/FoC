#!/usr/bin/env sh
# AI Assistance Disclosure:
# Tool: Codex (GPT-5), date: 2026-09-27
# Scope: Added a container regression check for JWT key build isolation.
# Author review: ZI YANG - verified correctness

set -eu

service_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
repository_dir=$(CDPATH= cd -- "$service_dir/.." && pwd)
context_dir=$(mktemp -d)
image_tag="foc-user-service-key-check:$$"

cleanup() {
	"${container_cli:-docker}" image rm -f "$image_tag" >/dev/null 2>&1 || true
	rm -rf "$context_dir"
}
trap cleanup EXIT

container_cli=${CONTAINER_CLI:-docker}

grep -Fx "keys.json" "$service_dir/.dockerignore" >/dev/null
if grep -Eq '^[[:space:]]*COPY[^#]*keys\.json' "$service_dir/Dockerfile"; then
	echo "Dockerfile must not copy keys.json into an image layer" >&2
	exit 1
fi
grep -Fx '      JWT_KEYSET_PATH: /run/secrets/keys.json' "$repository_dir/compose.yaml" >/dev/null
grep -Fx '      - ./user-service/keys.json:/run/secrets/keys.json:ro' "$repository_dir/compose.yaml" >/dev/null

tar -C "$service_dir" --exclude='keys.json' -cf - . | tar -C "$context_dir" -xf -
"$container_cli" build -q -t "$image_tag" "$context_dir" >/dev/null
"$container_cli" run --rm --entrypoint /bin/sh "$image_tag" -c '[ ! -e /app/keys.json ]'
