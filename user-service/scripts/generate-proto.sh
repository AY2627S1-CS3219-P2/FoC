#!/bin/sh
# AI Assistance Disclosure:
# Tool: Codex (GPT-5), date: 2026-09-29
# Scope: Added pinned, reproducible Go protobuf and gRPC code generation.
# Author review: ZI YANG - validated correctness

set -eu

service_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output_dir=${1:-$service_dir}
expected_protoc='libprotoc 35.1'
protovalidate_commit='d06a71c9ba254a56c10a7085e1dd44d33cedeb56'
protovalidate_sha256='27e32fd4f04c45c6f7d7f5b4fc2d8a480d2023558258a6b569bd3e687808c02a'
protoc_gen_go_version='v1.36.12'
protoc_gen_go_grpc_version='v1.6.2'

if ! command -v protoc >/dev/null 2>&1; then
	echo 'protoc is required' >&2
	exit 1
fi
if [ "$(protoc --version)" != "$expected_protoc" ]; then
	echo "protoc version must be $expected_protoc" >&2
	exit 1
fi

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/foc-user-proto.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM
mkdir -p "$work_dir/include/buf/validate" "$work_dir/bin" "$output_dir/internal/gen/user/v1"

schema_url="https://raw.githubusercontent.com/bufbuild/protovalidate/$protovalidate_commit/proto/protovalidate/buf/validate/validate.proto"
curl --fail --location --silent --show-error \
	--output "$work_dir/include/buf/validate/validate.proto" "$schema_url"
printf '%s  %s\n' "$protovalidate_sha256" "$work_dir/include/buf/validate/validate.proto" \
	| sha256sum --check --status

(
	cd "$service_dir"
	GOBIN="$work_dir/bin" go install "google.golang.org/protobuf/cmd/protoc-gen-go@$protoc_gen_go_version"
	GOBIN="$work_dir/bin" go install "google.golang.org/grpc/cmd/protoc-gen-go-grpc@$protoc_gen_go_grpc_version"
)

protoc \
	--proto_path="$service_dir/proto" \
	--proto_path="$work_dir/include" \
	--go_out="$output_dir" \
	--go_opt=module=foc/user-service \
	--go-grpc_out="$output_dir" \
	--go-grpc_opt=module=foc/user-service \
	--plugin="protoc-gen-go=$work_dir/bin/protoc-gen-go" \
	--plugin="protoc-gen-go-grpc=$work_dir/bin/protoc-gen-go-grpc" \
	user/v1/user.proto
