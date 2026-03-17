-- Proofs/Pipeline/Barrier.lean: Correctness proofs for barrier/switch
-- properties in the streammux packet routing model.

import Spec.Pipeline.Barrier

set_option linter.unusedVariables false

namespace Pipeline

-- Helper: BEq.beq on Nat is reflexive (n == n is always true).
private theorem nat_beq_self (n : Nat) : (n == n) = true := by
  simp [BEq.beq, Nat.beq_refl]

-- Helper: BEq.beq on Nat reflects equality.
private theorem nat_beq_false_of_ne {a b : Nat} (h : a ≠ b) : (a == b) = false := by
  simp [BEq.beq, Nat.beq_eq, h]

/-! ## Switch: current output always passes when no pending switch -/

/-- When there is no pending switch (`nextValue = none`), the current output
    always receives a pass decision, regardless of the packet content. -/
theorem switch_current_passes_stable
    (sw : SwitchState) (pkt : PipePacket)
    (h : sw.nextValue = none) :
    (outputSwitchGetState sw sw.currentValue pkt).2 = .pass := by
  unfold outputSwitchGetState
  rw [h]
  simp [nat_beq_self]

/-! ## Switch: post-commit target passes -/

/-- When a pending switch commits (keepUnless matches), the new target output
    receives a pass decision. The switch state is updated to reflect the
    committed value. -/
theorem switch_post_commit_target_passes
    (sw : SwitchState) (newOut : PipeOutputID) (pkt : PipePacket)
    (hNext : sw.nextValue = some newOut)
    (hDiff : newOut ≠ sw.currentValue)
    (hMatch : sw.keepUnless.match pkt = true) :
    (outputSwitchGetState sw newOut pkt).2 = .pass := by
  unfold outputSwitchGetState
  rw [hNext]
  simp [nat_beq_false_of_ne hDiff, hMatch, nat_beq_self]

/-! ## Switch: no dead zone -/

/-- For any packet, either the current output passes, OR there exists a pending
    next value whose commit would produce a pass. This guarantees that at
    least one output is always reachable — there is no "dead zone" where
    every output is dropped. -/
theorem switch_no_dead_zone
    (sw : SwitchState) (pkt : PipePacket) :
    (outputSwitchGetState sw sw.currentValue pkt).2 = .pass ∨
    ∃ next, sw.nextValue = some next ∧
            sw.keepUnless.match pkt = true ∧
            (outputSwitchGetState sw next pkt).2 = .pass := by
  unfold outputSwitchGetState
  cases hNv : sw.nextValue with
  | none =>
    left
    simp [nat_beq_self]
  | some next =>
    by_cases hEq : next = sw.currentValue
    · -- next = currentValue: current passes trivially
      left
      subst hEq
      simp [nat_beq_self]
    · -- next ≠ currentValue
      have hNeq : (next == sw.currentValue) = false := nat_beq_false_of_ne hEq
      by_cases hKu : sw.keepUnless.match pkt = true
      · -- keepUnless matches: commit happens, next becomes the target that passes
        right
        exact ⟨next, rfl, hKu, by simp [hNeq, hKu, nat_beq_self]⟩
      · -- keepUnless does not match: current still passes
        left
        have hKuF : sw.keepUnless.match pkt = false := Bool.eq_false_iff.mpr hKu
        simp [hNeq, hKuF, nat_beq_self]

/-! ## Syncer: current output passes -/

/-- The syncer always passes packets for the currently active output. -/
theorem syncer_current_passes
    (sw : SwitchState) :
    outputSyncerGetState sw sw.currentValue = .pass := by
  unfold outputSyncerGetState
  simp [nat_beq_self]

/-! ## Syncer: non-current output blocks -/

/-- The syncer blocks packets for any output that is not the currently active one. -/
theorem syncer_non_current_blocks
    (sw : SwitchState) (outputID : PipeOutputID)
    (h : sw.currentValue ≠ outputID) :
    outputSyncerGetState sw outputID = .block := by
  unfold outputSyncerGetState
  simp [nat_beq_false_of_ne h]

/-! ## Drop short-circuits fullBarrierCheck -/

/-- If the output switch returns drop, then `fullBarrierCheck` also returns
    drop — the syncer result is irrelevant. This models the short-circuit
    behavior where a dropped packet never reaches the syncer barrier. -/
theorem drop_short_circuits
    (switchSt syncerSt : SwitchState)
    (outputID : PipeOutputID) (pkt : PipePacket)
    (hDrop : (outputSwitchGetState switchSt outputID pkt).2 = .drop) :
    (fullBarrierCheck switchSt syncerSt outputID pkt).2 = .drop := by
  unfold fullBarrierCheck
  simp [hDrop]

end Pipeline
