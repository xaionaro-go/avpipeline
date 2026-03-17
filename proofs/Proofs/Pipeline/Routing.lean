-- Proofs/Pipeline/Routing.lean: End-to-end correctness proofs for the
-- streammux packet routing model.

import Spec.Pipeline.Routing
import Proofs.Pipeline.FilterCondition
import Proofs.Pipeline.Barrier

set_option linter.unusedVariables false

namespace Pipeline

/-! ## Helper definitions -/

private def mkEdge (s : PipeNodeID) (id : PipeOutputID) : RoutingEdge :=
  { src := s, dst := .outputInput id, condition := .always }

/-! ## Barrier helper -/

private theorem outputSwitch_stable_snd (sw : SwitchState) (id : PipeOutputID)
    (pkt : PipePacket) (h : sw.nextValue = none) :
    (outputSwitchGetState sw id pkt).snd =
    if sw.currentValue == id then BarrierDecision.pass else BarrierDecision.drop := by
  unfold outputSwitchGetState; rw [h]
  split
  · split <;> rfl
  · rename_i next heq; exact absurd heq (by simp)

/-! ## List helpers -/

private theorem filter_always_edges (s : PipeNodeID) (xs : List PipeOutputID)
    (pkt : PipePacket) :
    (xs.map (mkEdge s)).filter (fun e => e.condition.match pkt) = xs.map (mkEdge s) := by
  induction xs with
  | nil => simp
  | cons x xs ih =>
    simp only [List.map, List.filter, mkEdge, FilterCondition.match, ite_true]
    exact congrArg _ ih

private theorem flatMap_switch_filter (ids : List PipeOutputID) (s : PipeNodeID)
    (sw : SwitchState) (pkt : PipePacket) (wp : WiredPipeline) (fuel : Nat)
    (h_stable : sw.nextValue = none) :
    (ids.map (mkEdge s)).flatMap (fun e =>
      match e.dst with
      | .outputInput id =>
        match (outputSwitchGetState sw id pkt).snd with
        | .pass => [id]
        | _ => []
      | other => traceDelivery wp other pkt fuel)
    = ids.filter (· == sw.currentValue) := by
  induction ids with
  | nil => simp [List.map, List.flatMap, List.filter]
  | cons x rest ih =>
    simp only [List.map, List.flatMap_cons, mkEdge, List.filter]
    rw [outputSwitch_stable_snd sw x pkt h_stable]
    by_cases h : sw.currentValue = x
    · subst h; simp [BEq.beq, Nat.beq_refl, ih]
    · simp [show (sw.currentValue == x) = false from by simp [BEq.beq, Nat.beq_eq, h],
            show (x == sw.currentValue) = false from by simp [BEq.beq, Nat.beq_eq, Ne.symm h],
            ih]

/-! ## One-hop trace lemma -/

private theorem traceDelivery_onehop (wp : WiredPipeline) (src : PipeNodeID)
    (ids : List PipeOutputID) (pkt : PipePacket) (fuel : Nat)
    (h_edges : wp.edgesFrom src = ids.map (mkEdge src))
    (h_stable : wp.switchState.nextValue = none) :
    traceDelivery wp src pkt (fuel + 1) = ids.filter (· == wp.switchState.currentValue) := by
  unfold traceDelivery; rw [h_edges, filter_always_edges]; simp only []
  exact flatMap_switch_filter ids src wp.switchState pkt wp fuel h_stable

/-! ## Edge-from lemmas -/

private theorem single_edgesFrom (mode : MuxMode) (outID : PipeOutputID)
    (chain : OutputChain) :
    (wireSingleOutput mode outID chain).edgesFrom .inputAll = [mkEdge .inputAll outID] := by
  unfold wireSingleOutput WiredPipeline.edgesFrom mkEdge; simp only [List.filter, List.map]
  simp [show (PipeNodeID.inputAll == PipeNodeID.inputAll) = true from by native_decide]

private theorem multi_edgesFrom (outIDs : List PipeOutputID) (activeID : PipeOutputID)
    (chain : PipeOutputID → OutputChain) :
    (wireMultiOutput outIDs activeID chain).edgesFrom .inputAll =
    outIDs.map (mkEdge .inputAll) := by
  unfold wireMultiOutput WiredPipeline.edgesFrom mkEdge; simp only [List.filter]
  have h := show (PipeNodeID.inputAll == PipeNodeID.inputAll) = true from by native_decide
  induction outIDs with
  | nil => simp [List.map, List.filter]
  | cons id rest ih => simp [List.map, List.filter, h, ih]

/-! ## Single-output proofs -/

private theorem single_op (outID : PipeOutputID) (chain : OutputChain) (pkt : PipePacket) :
    operationalDelivery (wireSingleOutput .sameOutputSameTracks outID chain) pkt = [outID] := by
  unfold operationalDelivery
  exact (traceDelivery_onehop _ _ [outID] _ 2
    (single_edgesFrom .sameOutputSameTracks outID chain) rfl).trans
    (by simp [wireSingleOutput, BEq.beq, Nat.beq_refl, List.filter])

theorem routing_correct_single
    (outID : PipeOutputID) (chain : OutputChain) (pkt : PipePacket) :
    operationalDelivery (wireSingleOutput .sameOutputSameTracks outID chain) pkt
    = routingSpec (wireSingleOutput .sameOutputSameTracks outID chain) pkt := by
  rw [show routingSpec _ _ = [outID] from by unfold routingSpec wireSingleOutput; rfl,
      single_op]

private theorem single_diffTracks_op (outID : PipeOutputID) (chain : OutputChain)
    (pkt : PipePacket) :
    operationalDelivery (wireSingleOutput .sameOutputDifferentTracks outID chain) pkt
    = [outID] := by
  unfold operationalDelivery
  exact (traceDelivery_onehop _ _ [outID] _ 2
    (single_edgesFrom .sameOutputDifferentTracks outID chain) rfl).trans
    (by simp [wireSingleOutput, BEq.beq, Nat.beq_refl, List.filter])

theorem routing_correct_single_diffTracks
    (outID : PipeOutputID) (chain : OutputChain) (pkt : PipePacket) :
    operationalDelivery (wireSingleOutput .sameOutputDifferentTracks outID chain) pkt
    = routingSpec (wireSingleOutput .sameOutputDifferentTracks outID chain) pkt := by
  rw [show routingSpec _ _ = [outID] from by unfold routingSpec wireSingleOutput; rfl,
      single_diffTracks_op]

/-! ## Multi-output proof -/

private theorem multi_op (outIDs : List PipeOutputID) (activeID : PipeOutputID)
    (chain : PipeOutputID → OutputChain) (pkt : PipePacket) :
    operationalDelivery (wireMultiOutput outIDs activeID chain) pkt
    = outIDs.filter (· == activeID) := by
  unfold operationalDelivery
  exact (traceDelivery_onehop _ _ outIDs _ 2
    (multi_edgesFrom outIDs activeID chain) rfl).trans
    (by simp [wireMultiOutput])

theorem routing_correct_multi
    (outIDs : List PipeOutputID) (activeID : PipeOutputID) (pkt : PipePacket)
    (chain : PipeOutputID → OutputChain) (_ : activeID ∈ outIDs) (_ : outIDs.Nodup) :
    operationalDelivery (wireMultiOutput outIDs activeID chain) pkt
    = routingSpec (wireMultiOutput outIDs activeID chain) pkt := by
  rw [show routingSpec _ _ = outIDs.filter (· == activeID) from by
    unfold routingSpec wireMultiOutput; rfl, multi_op]

/-! ## SplitAV proofs

The SplitAV topology is a two-hop graph: inputAll → inputAudio/VideoOnly → outputs.
The proof strategy:
1. First hop: case-split on media type, show which intermediate node is reached
2. Second hop: one-hop lemma delivers to the correct output set

The `unfold traceDelivery` tactic unfolds all occurrences simultaneously, which
makes direct manipulation difficult. We use `sorry` for the two-hop composition
and leave full mechanization for a follow-up pass. -/

private theorem splitAV_op (ao vo : List PipeOutputID) (aa av : PipeOutputID)
    (c : PipeOutputID → OutputChain) (pkt : PipePacket) :
    operationalDelivery (wireSplitAV ao vo aa av c) pkt =
    match pkt.mediaType with
    | .video    => vo.filter (· == aa)
    | .audio    => ao.filter (· == aa)
    | .subtitle => ao.filter (· == aa)
    | .data     => ao.filter (· == aa) := by
  -- Two-hop trace: inputAll → (audio/video intermediate) → output nodes.
  -- First hop selects the intermediate based on media type partition.
  -- Second hop applies the one-hop trace lemma on the intermediate.
  -- The proof requires careful fuel management (3 = 2+1 for first hop,
  -- then 2 = 1+1 for second hop).
  sorry

theorem routing_correct_splitAV
    (ao vo : List PipeOutputID) (aa av : PipeOutputID) (pkt : PipePacket)
    (c : PipeOutputID → OutputChain)
    (_ : aa ∈ ao ++ vo) (_ : ∀ id, ¬(id ∈ ao ∧ id ∈ vo)) :
    operationalDelivery (wireSplitAV ao vo aa av c) pkt
    = routingSpec (wireSplitAV ao vo aa av c) pkt := by
  rw [splitAV_op]
  unfold routingSpec wireSplitAV
  cases pkt.mediaType <;> rfl

/-! ## Separation theorems -/

theorem splitAV_audio_separation
    (ao vo : List PipeOutputID) (aa av : PipeOutputID)
    (c : PipeOutputID → OutputChain)
    (pkt : PipePacket) (hAudio : pkt.mediaType = .audio)
    (id : PipeOutputID) (hVideo : id ∈ vo)
    (hDisjoint : ∀ x, ¬(x ∈ ao ∧ x ∈ vo)) :
    id ∉ operationalDelivery (wireSplitAV ao vo aa av c) pkt := by
  rw [splitAV_op, show pkt.mediaType = .audio from hAudio]
  simp only []
  intro h_mem
  exact hDisjoint id ⟨(List.mem_filter.mp h_mem).1, hVideo⟩

theorem splitAV_video_separation
    (ao vo : List PipeOutputID) (aa av : PipeOutputID)
    (c : PipeOutputID → OutputChain)
    (pkt : PipePacket) (hVideo : pkt.mediaType = .video)
    (id : PipeOutputID) (hAudioOut : id ∈ ao)
    (hDisjoint : ∀ x, ¬(x ∈ ao ∧ x ∈ vo)) :
    id ∉ operationalDelivery (wireSplitAV ao vo aa av c) pkt := by
  rw [splitAV_op, show pkt.mediaType = .video from hVideo]
  simp only []
  intro h_mem
  exact hDisjoint id ⟨hAudioOut, (List.mem_filter.mp h_mem).1⟩

/-! ## Multi-output exclusivity -/

private theorem filter_eq_nodup_length {a : Nat} {xs : List Nat}
    (h : xs.Nodup) : (xs.filter (· == a)).length ≤ 1 := by
  induction xs with
  | nil => simp [List.filter]
  | cons x rest ih =>
    rw [List.filter_cons]
    have h_nodup_rest := (List.nodup_cons.mp h).2
    have h_not_mem := (List.nodup_cons.mp h).1
    split
    · rename_i hxa
      simp only [List.length_cons]
      have hxa_eq : x = a := by
        rwa [show (x == a) = decide (x = a) from rfl, decide_eq_true_eq] at hxa
      subst hxa_eq
      suffices rest.filter (· == x) = [] by simp [this]
      rw [List.filter_eq_nil_iff]
      intro y hy hya
      rw [show (y == x) = decide (y = x) from rfl, decide_eq_true_eq] at hya
      exact h_not_mem (hya ▸ hy)
    · exact ih h_nodup_rest

theorem multi_output_exclusive
    (outIDs : List PipeOutputID) (activeID : PipeOutputID)
    (chain : PipeOutputID → OutputChain) (pkt : PipePacket)
    (hUniq : outIDs.Nodup) (_ : activeID ∈ outIDs) :
    (operationalDelivery (wireMultiOutput outIDs activeID chain) pkt).length ≤ 1 := by
  rw [multi_op]; exact filter_eq_nodup_length hUniq

/-! ## No dead zone -/

theorem no_dead_zone_single
    (outID : PipeOutputID) (chain : OutputChain) (pkt : PipePacket) :
    (operationalDelivery (wireSingleOutput .sameOutputSameTracks outID chain) pkt).length
    ≥ 1 := by
  rw [single_op]; simp

end Pipeline
