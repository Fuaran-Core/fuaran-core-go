# Decisions

Each entry records a decision about this module, why it was taken, and what it rules out.

## D1 — 2026-10-24 — the repository, its organisation, its licence, and how a host depends on it

- `fuaran-core-go` lives at `Fuaran-Core/fuaran-core-go`, beside the reference implementation it
  twins (`Fuaran-Core/fuaran-core`) and the Rust spine (`Fuaran-Core/fuaran-core-rs`), and is
  Apache-2.0 and public from its first commit, for the reason the reference is: a spine every host
  builds on is only useful if every host can hold it. (Operator ruling 2026-10-24, recorded by
  Phase 485; the reference records it as its own decision.)
- The module path is `github.com/Fuaran-Core/fuaran-core-go`, Go 1.26, and it takes no dependency
  beyond the standard library. It is unversioned (no `v*` tag) until a release is a deliberate act.
- **A host depends on it by `require` at a pseudo-version of a commit of this repository**, the
  shape `go get` resolves from the public repository with no path on disk in the way: the Go UI host
  requires `github.com/Fuaran-Core/fuaran-core-go v0.0.0-<time>-<commit>`, and `go get
  github.com/fuaran-ui/fuaran-go` works for a reader who has neither checkout. A committed
  `replace` with a relative path was declined: the two repositories sit side by side in one layout
  and not in another (a worktree, a CI runner), so a path that resolves in one is a broken build in
  the other. Local development against an unpushed spine uses a `go.work` the host does not commit.
  CI needs no checkout of this repository for that reason.

## D2 — 2026-10-24 — what the spine holds, and what it does not (a premise checked)

Phase 485 was written as an EXTRACTION of the spine out of the Go UI host. Checked against that host,
the premise holds for part of it and not for the rest:

- **Moved, verbatim in behaviour:** the canonical writer's primitives — the string escape, the number
  form, the UTF-16 ordinal key order (`json`) — and the exact decimal with its seven rounding modes
  (`decimal`). The UI host re-exports each under the paths it already published, and its Compute
  twin forwards to the decimal.
- **Taken over, generalised:** the hash-chain's pre-image and link. The UI host's typed op-record
  chain spliced its own envelope into `{"seq":…,"actor":…,"op":…}` and hashed `prev + "|" +
  payload`; that is exactly the reference's chain over OPAQUE op text (`chain`), so the host's
  `ComputeHash` now computes through the spine's `chain.Link` and `chain.Payload`, and its one
  canonical actor encoding is the spine's `chain.EncodeActor`.
- **Not moved, because there was nothing of the kind to move:** the UI host parses JSON with the
  standard library's `encoding/json` and enforces its limits over the parsed value, so it has no
  strict reader to extract; its apply engine works over its own typed node and op vocabulary; and
  its DAG module is the codec of a UI record (it mints and verifies no content id). They stay in that
  host. What the spine holds instead is the reference's own contract, written for this module to the
  reference's vectors: the strict reader with its limits record and the reference's `JsonErrorKind`
  (`json`), the decode error contract and its cursor combinators over the null-free model
  (`decode`), the content address (`address`), the skeleton tree and its six ops (`tree`, `ops`),
  and the content-addressed DAG with its orders, ranks, replays and integrity walk (`dag`), generic
  over the op.
- *Declined:* moving the UI host's `wire` decode onto the spine's reader in the same change. That
  host's decode-tolerance contract is the shared wire corpus's, not the reference's, and the change
  would be a behavioural one across sixty thousand lines certified against another corpus. A later
  change may make the host's `wire` read through the spine's reader with the wire's own limits; it is
  not this one's.

## D3 — 2026-10-24 — where the vectors come from, and that the oracle is part of the gate

- The conformance legs read the reference's `conformance/` directory from a Core checkout at the
  commit `core-pin.json` names (found at `FUARAN_CORE_DIR`, or as `fuaran-core/` beside this
  repository or an ancestor of it). A missing checkout FAILS the legs: a suite that skipped its
  vectors would read green having certified nothing. The pin moves only as a deliberate act, in the
  change that needs it, with the legs re-run against the new commit.
- The oracle differential is part of the same gate, not an opt-in: `run.ps1` builds the reference's
  oracle process from that checkout (or takes `FUARAN_CORE_ORACLE`), the deterministic differential
  runs on every `go test` from a stated seed over a stated input count — the two numbers
  `host-compliance.json` declares — and a bounded coverage-guided run of each native fuzz target
  follows. A missing oracle fails rather than skips, for the reason above. This is the first host
  to declare `oracle-differential` evidence, and the differential found three divergences in the
  spine's first reader before any vector did: the position a refusal is reported at (the reference
  refuses a number at the END of its token and a lone surrogate AFTER the unit that follows it),
  the token scan (the reference's scan takes each part of a number as far as it runs and then judges
  the grammar, so `1e5-0` is `1e5` and trailing text, not one malformed token), and `expect`, which
  refuses `ExpectedToken` at the end of the input where a value was not what was expected. The
  reader now reads as the model reads, and the differential compares positions on ASCII input.
  The bounded fuzz run then found a fourth, in the apply: on a tree that repeats an id — outside
  the well-formedness the reference's apply assumes and never states — the reference's
  `updateNode` rebuilds EVERY node carrying the id while a first-match walk rebuilds one, so the
  spine's tree primitives are now the model's clause for clause (`ins`, `rem_at`, `reorder_at`,
  `upd` over every node carrying the id; `tryFind` and `parentOf` the first in pre-order), and the
  two agree on every tree the fuzzer builds, well-formed or not. A fifth followed in the reader: a
  `\u` escape with fewer than four UTF-16 units left is `TruncatedUnicodeEscape` to the reference,
  which counts units, where a byte count saw four bytes in `\u0é0` and called it `BadHexDigit`; the
  reader now counts units. Each failing input is kept as a regression seed under
  `conformance/testdata/fuzz/`, and the bounded runs that found them are the gate's.

## D4 — 2026-10-24 — what the first certification covers, and what it does not yet

Certified against the reference's vectors at the Core commit `core-pin.json` names, every count
asserted by the leg that runs it (`conformance/`); the reference's adoption records carry these
counts under the host key `fuaran-core-go`:

| Family | Run | Not run, and why |
|---|---|---|
| `refusals/codec-refusals.json` | the 56 `json` vectors | the `column` and `profile` vectors: the columnar codec and the wire-profile reader are not the spine's yet |
| `decode/decode-rejects.json` | all 47 | — |
| `laws/decimal-laws.json` | the 53 `canonical`, `compare`, `add`, `toFloat` vectors | `codecDecode`, `codecEncode`, `aggregate`: the column codec and the aggregates |
| `escape/string-escape.json` | all 38 lines | — (no manifest, so no adoption record; recorded here) |
| `apply/skeleton-apply.json` | all 25 | — |
| `chain/skeleton-chain.json` | all 16 | — |
| `dag/skeleton-dag.json` | all 11 | — |
| `premises/` | every vector of all five probes | — |
| `refusals/seam-refusals.json`, `merge/`, `diff/`, the capability, field, propagation and canonical-form laws | — | the seams, the reconcile and diff, the capability contract, the schema field and unit, propagation and the IDL canonical form are not in the spine yet |

A rejection is answered in the reference's own vocabulary (its canonical rejection bytes), not
mapped to another host's codes, because this module IS the reference's twin. The `encoding/` table
is declined as the Rust spine declined it: it has no manifest, and what it pins beyond the escapes
is a profile of stores this spine has never read or minted.

`host-compliance.json` declares every row the reference's evidence map offers: `vector-certified`
where a leg runs the family, `oracle-differential` for `json.parse` (`JsonParse`), `tree.apply`
(`TreeOps`) and `chain.verify` (`Chain`), `host-premise` on the five premise rows, and
`not-covered` with the reason on the rest. It validates with the reference's `compliance` verb and
reads `compliant`, with 36 of the 193 offered rows at parity: the 27 `Chain` rows (`skeletonChain`,
`skeletonDag` and the `chain.verify` differential), the 4 `DecodeLayer` rows (`decodeRejects`) and the
5 host-premise rows. Every other offered row carries at least one family or function this spine does
not answer yet — the `column` and `profile` codec refusals under `JsonParse` and `WireColumn`, the
`dag.fold` and `wire.decode` and `wire.render` differentials, and the families of the last row of the
table above. The remaining families are the next change's; this entry holds no promise of when.
