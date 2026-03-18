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
2. Second hop: one-hop lemma delivers to the correct output set -/

/-! ### SplitAV edge-from lemmas -/

/-- Helper: filtering edges by src through a mapped list where all elements
    have a different src yields []. -/
private theorem filter_map_src_ne {src other : PipeNodeID}
    (h : (other == src) = false)
    (xs : List PipeOutputID)
    (mkE : PipeOutputID → RoutingEdge)
    (hmk : ∀ id, (mkE id).src = other) :
    (xs.map mkE).filter (fun e => e.src == src) = [] := by
  induction xs with
  | nil => simp
  | cons x rest ih =>
    simp only [List.map, List.filter, hmk, h, ite_false, ih]

/-- Helper: filtering edges by src through a mapped list where all elements
    have that src yields the original mapped list. -/
private theorem filter_map_src_eq {src : PipeNodeID}
    (h : (src == src) = true)
    (xs : List PipeOutputID)
    (mkE : PipeOutputID → RoutingEdge)
    (hmk : ∀ id, (mkE id).src = src) :
    (xs.map mkE).filter (fun e => e.src == src) = xs.map mkE := by
  induction xs with
  | nil => simp
  | cons x rest ih =>
    simp only [List.map, List.filter, hmk, h, ite_true, ih]

/-- `edgesFrom .inputAll` in a splitAV pipeline returns only the two split edges. -/
private theorem splitAV_edgesFrom_inputAll (ao vo : List PipeOutputID)
    (aa av : PipeOutputID) (c : PipeOutputID → OutputChain) :
    (wireSplitAV ao vo aa av c).edgesFrom .inputAll =
    [ { src := .inputAll, dst := .inputAudioOnly, condition := audioSubtitleDataCond }
    , { src := .inputAll, dst := .inputVideoOnly, condition := videoCond } ] := by
  unfold wireSplitAV WiredPipeline.edgesFrom
  simp only [List.filter_append, List.filter, List.map]
  have hAA : (PipeNodeID.inputAll == PipeNodeID.inputAll) = true := by native_decide
  have hAuA : (PipeNodeID.inputAudioOnly == PipeNodeID.inputAll) = false := by native_decide
  have hVA : (PipeNodeID.inputVideoOnly == PipeNodeID.inputAll) = false := by native_decide
  simp only [hAA, ite_true]
  rw [filter_map_src_ne hAuA ao _ (fun _ => rfl),
      filter_map_src_ne hVA vo _ (fun _ => rfl)]
  simp

/-- `edgesFrom .inputAudioOnly` returns the audio output edges. -/
private theorem splitAV_edgesFrom_audioOnly (ao vo : List PipeOutputID)
    (aa av : PipeOutputID) (c : PipeOutputID → OutputChain) :
    (wireSplitAV ao vo aa av c).edgesFrom .inputAudioOnly =
    ao.map (mkEdge .inputAudioOnly) := by
  unfold wireSplitAV WiredPipeline.edgesFrom mkEdge
  simp only [List.filter_append, List.filter, List.map]
  have hAAu : (PipeNodeID.inputAll == PipeNodeID.inputAudioOnly) = false := by native_decide
  have hAuAu : (PipeNodeID.inputAudioOnly == PipeNodeID.inputAudioOnly) = true := by native_decide
  have hVAu : (PipeNodeID.inputVideoOnly == PipeNodeID.inputAudioOnly) = false := by native_decide
  simp only [hAAu, ite_false, List.nil_append]
  rw [filter_map_src_eq hAuAu ao _ (fun _ => rfl),
      filter_map_src_ne hVAu vo _ (fun _ => rfl)]
  simp

/-- `edgesFrom .inputVideoOnly` returns the video output edges. -/
private theorem splitAV_edgesFrom_videoOnly (ao vo : List PipeOutputID)
    (aa av : PipeOutputID) (c : PipeOutputID → OutputChain) :
    (wireSplitAV ao vo aa av c).edgesFrom .inputVideoOnly =
    vo.map (mkEdge .inputVideoOnly) := by
  unfold wireSplitAV WiredPipeline.edgesFrom mkEdge
  simp only [List.filter_append, List.filter, List.map]
  have hAV : (PipeNodeID.inputAll == PipeNodeID.inputVideoOnly) = false := by native_decide
  have hAuV : (PipeNodeID.inputAudioOnly == PipeNodeID.inputVideoOnly) = false := by native_decide
  have hVV : (PipeNodeID.inputVideoOnly == PipeNodeID.inputVideoOnly) = true := by native_decide
  simp only [hAV, ite_false, List.nil_append]
  rw [filter_map_src_ne hAuV ao _ (fun _ => rfl),
      List.nil_append,
      filter_map_src_eq hVV vo _ (fun _ => rfl)]

/-! ### SplitAV second-hop lemma

The second hop of the splitAV trace goes from an intermediate node
(inputAudioOnly or inputVideoOnly) to the output nodes. This is exactly
the one-hop trace pattern. -/

private theorem splitAV_secondHop_audio (ao vo : List PipeOutputID)
    (aa av : PipeOutputID) (c : PipeOutputID → OutputChain) (pkt : PipePacket)
    (fuel : Nat) :
    traceDelivery (wireSplitAV ao vo aa av c) .inputAudioOnly pkt (fuel + 1)
    = ao.filter (· == aa) := by
  exact traceDelivery_onehop _ _ ao _ fuel
    (splitAV_edgesFrom_audioOnly ao vo aa av c) rfl

private theorem splitAV_secondHop_video (ao vo : List PipeOutputID)
    (aa av : PipeOutputID) (c : PipeOutputID → OutputChain) (pkt : PipePacket)
    (fuel : Nat) :
    traceDelivery (wireSplitAV ao vo aa av c) .inputVideoOnly pkt (fuel + 1)
    = vo.filter (· == aa) := by
  exact traceDelivery_onehop _ _ vo _ fuel
    (splitAV_edgesFrom_videoOnly ao vo aa av c) rfl

/-! ### SplitAV two-hop composition

The proof decomposes traceDelivery at inputAll into two hops:
1. First hop selects the intermediate node based on media type
2. Second hop applies the one-hop lemma from intermediate to outputs

We avoid `unfold traceDelivery` (which unfolds all occurrences) by using
`conv_lhs => unfold traceDelivery` to unfold only the outermost call. -/

private theorem splitAV_op (ao vo : List PipeOutputID) (aa av : PipeOutputID)
    (c : PipeOutputID → OutputChain) (pkt : PipePacket) :
    operationalDelivery (wireSplitAV ao vo aa av c) pkt =
    match pkt.mediaType with
    | .video    => vo.filter (· == aa)
    | .audio    => ao.filter (· == aa)
    | .subtitle => ao.filter (· == aa)
    | .data     => ao.filter (· == aa) := by
  unfold operationalDelivery
  show traceDelivery (wireSplitAV ao vo aa av c) .inputAll pkt 3 = _
  -- Unfold the outermost traceDelivery once using the equation lemma.
  -- traceDelivery.eq_2: traceDelivery wp src pkt (fuel'+1) = flatMap over filtered edges
  -- fuel 3 = 2.succ, so fuel' = 2 and the recursive calls use fuel' = 2.
  rw [show (3 : Nat) = Nat.succ 2 from rfl, traceDelivery.eq_2]
  -- Rewrite edgesFrom to the two split edges.
  rw [splitAV_edgesFrom_inputAll]
  -- Case-split on media type to evaluate the filter conditions.
  -- The partition lemmas give us the condition match results, and we
  -- use them to rewrite in the goal before applying the second-hop lemma.
  cases hmt : pkt.mediaType
  · -- video: videoCond matches, audioSubtitleDataCond doesn't
    have ⟨hv, ha⟩ := partition_video pkt hmt
    -- The filter over the two-element edge list evaluates condition.match pkt
    -- for each edge. We need to unfold the list operations.
    simp only [List.filter, ha, hv, ite_true, ite_false,
               List.flatMap_cons, List.flatMap_nil, List.append_nil]
    exact splitAV_secondHop_video ao vo aa av c pkt 1
  · -- audio: audioSubtitleDataCond matches, videoCond doesn't
    have ⟨hv, ha⟩ := partition_audio pkt hmt
    simp only [List.filter, ha, hv, ite_true, ite_false,
               List.flatMap_cons, List.flatMap_nil, List.append_nil]
    exact splitAV_secondHop_audio ao vo aa av c pkt 1
  · -- subtitle: audioSubtitleDataCond matches, videoCond doesn't
    have ⟨hv, ha⟩ := partition_subtitle pkt hmt
    simp only [List.filter, ha, hv, ite_true, ite_false,
               List.flatMap_cons, List.flatMap_nil, List.append_nil]
    exact splitAV_secondHop_audio ao vo aa av c pkt 1
  · -- data: audioSubtitleDataCond matches, videoCond doesn't
    have ⟨hv, ha⟩ := partition_data pkt hmt
    simp only [List.filter, ha, hv, ite_true, ite_false,
               List.flatMap_cons, List.flatMap_nil, List.append_nil]
    exact splitAV_secondHop_audio ao vo aa av c pkt 1

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
