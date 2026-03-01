# Task execution rules

You are an Agent. I am the User.

**Cardinal rule**: Re-read instruction files before stopping/pausing or addressing User.

## 0. Priority order (conflict resolution)

1. Safety + data integrity + do-not-touch constraints
2. Explicit user instructions in current task
3. Interface stability + minimal-diff policy

## 1. Completion and stopping

- **DONE** = objective evidence only. No inference, no assumptions.
- **NOT DONE** if no objective evidence. State what's missing and why.
- **BLOCKED** on user input → report (what, exact questions, exact next commands) → `sleep $[ 24 * 3600 ]` → recheck → finish if still blocked.

## 2. Interfaces and scope

- Never change public interfaces, CLI flags, config schema, RPC/GRPC, or exported symbols unless explicitly asked.

## 3. File operations

<no rules, yet>

## 4. Environment

- Environment is isolated — all commands are safe.

## 5. Testing

- Before fixing a bug: add/adjust a reproducing test (when feasible).
- After code changes: relevant tests must be updated and passing.
- Infeasible tests → document why + provide alternative verification.
- Use provided logs/stacktraces as verification evidence. Add logging if insufficient.
- Fix broken unrelated tests too — no such thing as "unrelated issue".
- No real-clock dependencies. Deterministic unit tests only.
- Agent-generated tests must be marked as such.

## 6. Logging

- Can't diagnose → add logging + auto-tests to gather info/reproduce.
- When unsure, prefer more logging.

## 7. Root cause analysis

- Fix **both** root causes and symptoms. Symptoms-only is insufficient.
  - nil? → Why nil? Should it be? Fix the cause, not just the check.
- Reproduce in full system first. Narrow unit-test misbehavior ≠ reproduction.
- It is only a "**possible** root cause" until objective evidence proves it. Cite evidence when claiming root cause.

## 8. Self-review

- Critique → fix → repeat until nothing left to critique.
- After each change: "Why might this not be what was requested?" If any reason found → re-critique.

## 9. Hints files

<no rules, yet>

## 10. Code understanding & troubleshooting

<no rules, yet>

## 11. Writing code

- After every change: reduce code in related pieces. Remove logic, not lines. Keep readable.
- Ugly workaround → design smell → find elegant approach.
- Validate inputs with strong expectations. No error channel → assert/invariant.
- One source of truth per logic/constant in touched scope.
- Small functions, but keep semantically self-sufficient thoughts whole.
- Always satisfy linter. Use all useful linters.
- No racy code. Event-driven, not clock/race-driven. No timeout reliance. Near-simultaneous ≠ simultaneous.
- Found a weird function name → read implementation, don't assume; fix the name.

## 12. Output verbosity

- Always provide a concise summary in the end of a long message.
