#!/bin/sh
# AI Assistance Disclosure:
# Tool: Codex (GPT-5), date: 2026-09-29
# Scope: Added a check that rejects stale user-service protobuf output.
# Author review: ZI YANG - validated correctness

set -eu

service_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
check_dir=$(mktemp -d "${TMPDIR:-/tmp}/foc-user-proto-check.XXXXXX")
trap 'rm -rf "$check_dir"' EXIT HUP INT TERM

"$service_dir/scripts/generate-proto.sh" "$check_dir"

for generated_name in user.pb.go user_grpc.pb.go
do
	committed="$service_dir/internal/gen/user/v1/$generated_name"
	regenerated="$check_dir/internal/gen/user/v1/$generated_name"
	if ! cmp --silent "$committed" "$regenerated"; then
		echo "$committed is stale; run scripts/generate-proto.sh" >&2
		exit 1
	fi
done
