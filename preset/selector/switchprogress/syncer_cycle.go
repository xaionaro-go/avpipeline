package switchprogress

type SyncerCycle struct {
	gate       *Gate
	generation uint64
}

func (g *Gate) BeginSyncerCycle() SyncerCycle {
	generation := g.nextSyncerGeneration.Add(1)
	for {
		current := g.syncerGeneration.Load()
		if current == 0 {
			if g.syncerGeneration.CompareAndSwap(0, generation) {
				g.inFlight.Add(1)
				return SyncerCycle{
					gate:       g,
					generation: generation,
				}
			}
			continue
		}
		if g.syncerGeneration.CompareAndSwap(current, generation) {
			return SyncerCycle{
				gate:       g,
				generation: generation,
			}
		}
	}
}

func (c SyncerCycle) Release() bool {
	if c.gate == nil || c.generation == 0 {
		return false
	}
	return c.gate.releaseSyncerCycle(c.generation)
}

func (g *Gate) releaseSyncerCycle(
	expectedGeneration uint64,
) bool {
	for {
		current := g.syncerGeneration.Load()
		if current == 0 {
			return false
		}
		if expectedGeneration != 0 && current != expectedGeneration {
			return false
		}
		if !g.syncerGeneration.CompareAndSwap(current, 0) {
			continue
		}
		g.inFlight.Add(-1)
		return true
	}
}
