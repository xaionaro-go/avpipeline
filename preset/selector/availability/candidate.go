package availability

// Candidate is one priority slot in an availability scan.
type Candidate struct {
	Present bool
	Source  Source
}

// Absent returns a sparse or unavailable priority slot.
func Absent() Candidate {
	return Candidate{}
}

// Present returns a priority slot whose source may opt into availability checks.
func Present(source Source) Candidate {
	return Candidate{
		Present: true,
		Source:  source,
	}
}
