# fuaran-core-go

The Fuaran Core spine in Go: the pieces every Go host of a Fuaran wire builds on, with nothing of
any one wire's vocabulary in them.

- `json` — the value model, the strict reader with its limits record and the reference's closed
  refusal classes, and the canonical writer (the escape, the number form, the ordinal key order);
- `decode` — the decode error contract and the reading combinators over the null-free value model;
- `address` — SHA-256 as hex and the `sha256:` content address;
- `decimal` — the exact decimal and its seven rounding modes;
- `tree` and `ops` — the skeleton tree and its six ops' apply, with the op-script footprint;
- `chain` — the op-stream chain over opaque payloads, and the actor;
- `dag` — the content-addressed op-DAG, its orders, ranks, replays and integrity walk;
- `oracle` — a client of the reference's oracle protocol, for a host's own differential.

Each is the twin of the reference implementation ([fuaran-core](https://github.com/Fuaran-Core/fuaran-core))
and is certified against the conformance vectors it emits (`conformance/`, one file per group of
families; [DECISIONS.md](DECISIONS.md) D4 says which) and differentially against its extracted proof
models through the oracle process (D3). It declares its compliance row by row in
[`host-compliance.json`](host-compliance.json), which the reference's `compliance` verb validates.

```go
import "github.com/Fuaran-Core/fuaran-core-go/json"

v, refusal := json.Parse(`{"b":1,"a":[true,"x"]}`)
if refusal != nil { /* a *json.Error (malformed text) or a *json.LimitBreach */ }
canonical := json.Render(v) // {"a":[true,"x"],"b":1}
```

Standard library only; Go 1.26.

## Build

```powershell
pwsh ./run.ps1
```

`run.ps1` runs gofmt, vet, build and the suite, then a bounded run of each oracle fuzz target. The
suite reads the reference's vectors from a Core checkout at the commit [`core-pin.json`](core-pin.json)
names — `FUARAN_CORE_DIR`, or `fuaran-core/` beside this repository — and builds the reference's
oracle process from it; both FAIL when absent rather than skip, because a skipped leg certifies nothing.
`-SkipFuzz` and `-FuzzSeconds <n>` govern the fuzz run.

## Licence

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
