# api/

`openapi.yaml` goes here.

Empty by design. Root `AGENTS.md` §8 makes the spec the contract and D-005
makes it spec-first: a human authors `openapi.yaml`, then `make generate`
produces Go types and the server interface into `internal/gen/`. An agent
implements against it; it does not write it.
