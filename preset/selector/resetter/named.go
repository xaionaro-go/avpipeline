package resetter

// Named pairs a resetter with stable reset diagnostics.
type Named struct {
	Name     string
	Resetter Resetter
}
