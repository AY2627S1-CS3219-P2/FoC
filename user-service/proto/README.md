<!--
AI Assistance Disclosure:
Tool: Codex (GPT-5), date: 2026-09-29
Scope: Documented the pinned user-service protobuf generation workflow.
Author review: ZI YANG - verified correctness
-->

# User-service protobuf generation

Run generation from anywhere inside the repository with:

```sh
user-service/scripts/generate-proto.sh
```

The command requires `protoc` 35.1. It installs the exact Go plugin versions
pinned in the generation script into a temporary directory. The Protovalidate
schema is downloaded from the pinned commit and verified against its pinned
SHA-256 digest. Generated Go files are written under
`internal/gen/user/v1/` and must not be edited manually.

Check that committed output is current without changing it:

```sh
user-service/scripts/verify-generated.sh
```
