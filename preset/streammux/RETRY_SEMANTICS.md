# StreamMux retry semantics

## Layers

Two reconnect layers exist; each owns a distinct fault era and they
run serially, never in parallel.

| Layer | Owns | Trigger | Cadence | Termination |
|---|---|---|---|---|
| `kernel.Retryable[K]` | A single live `K` (e.g. `*kernel.Output`) | SendInput / Generate / kernel-open errors on the SAME destination | Caller-controlled (ffstream uses 100 ms sleep + caller-supplied hard timeout via `SenderTemplate.RetryOutputTimeoutOnFailure`) | When the caller-supplied timeout elapses, the hard error propagates to the node layer which forwards it as a `node.Error` |
| `streammux` eviction + recreate | The Output[C] routing wrapper (encoder + barriers + send-node + per-input switch state) | `node.Error` arriving at `handleOutputNodeError` for any output (active or non-active) | 1 Hz tick + on-eviction trigger | Indefinite while the orphaned input still has `OutputSwitch.CurrentValue == math.MinInt32` |

## SSOT

Each layer reconnects exactly the entities it owns. The kernel layer
cannot rebuild routing state (encoder factories, per-input switches,
sibling-failover); the routing layer cannot reconnect a live FFmpeg
context (it must Close + Factory a fresh one).

`SenderTemplate.RetryOutputTimeoutOnFailure` chooses which layer
absorbs transient destination-side faults:

- `0` (default) — kernel-Retryable is bypassed (`newOutput`). Every
  destination fault evicts the routing-layer Output[C] and the
  recreate path rebuilds it. **One layer, one cadence (1 Hz).**
- `>0` — kernel-Retryable owns the first `RetryOutputTimeoutOnFailure`
  worth of fault era at 100 ms cadence. After the hard timeout the
  fault era escalates to the routing layer, which then takes over at
  1 Hz indefinitely.

## Recreate cadence

The recreate-on-orphaned-input path retries at exactly 1 Hz per
orphaned SenderKey, indefinitely, with no budget. Rationale: IRL
streaming users cannot tolerate any "give-up" point — the camera
should reconnect the moment the destination becomes reachable. The
1 Hz floor is the only rate-limit; it stops the daemon from spinning
on `connect()` syscalls when the destination is hard-down.

## What we deliberately do NOT do

- No exponential backoff. Backoff stretches reconnect latency
  proportionally to outage length; for a 10-minute outage the user
  would wait the cap (e.g. 60 s) every cycle on top of the outage.
  Live-streaming UX requires sub-second recovery, full stop.
- No `MaxAttempts` retirement. A "permanently failed" SenderKey is
  meaningless for an IRL streamer; the destination either becomes
  reachable or doesn't, and giving up wedges the camera path until a
  manual reconfigure.
- No `MaxAge` sliding-window reset. With no latch to clear, MaxAge
  has nothing to reset.

## Sibling-failover (the ONE thing eviction layer adds over Retryable)

When an output errors and another sibling Output is attached to the
same input, `recommitDemotedInputToSibling` switches the input onto
the sibling immediately — no recreate, no 1 Hz wait. The recreate
path only fires when there is no sibling. This case has no kernel-
Retryable equivalent; sibling-failover is why the eviction layer
must exist.
