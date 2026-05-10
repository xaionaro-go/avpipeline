# Development Rules

These rules apply to all changes in this repository.

## Existing Instructions

- Read and follow `.github/instructions/*.instructions.md` before changing code.
- Resolve conflicts by following the most specific instruction for the touched code.

## Scope

- Keep public APIs, CLI flags, config schemas, RPC/GRPC contracts, and exported symbols stable unless the task explicitly requires a change.
- Prefer small, local changes that fit the existing package boundaries.
- Add or update deterministic tests for behavior changes when feasible.

## Logging

- Use trace logging only (`logger.Trace*`) for any log site emitted once per frame, once per packet, or inside frame/packet processing loops.
- Do not use non-trace logging (`logger.Debug*`, `logger.Info*`, `logger.Warn*`, `logger.Error*`, `logger.Panic*`, `logger.Fatal*`) for expected per-frame/per-packet events.
- If a hot-path condition needs higher visibility, aggregate it with counters or emit a rate-limited summary outside the per-frame/per-packet path.
