package readstate

// ValidateManifestTransition is PDSReadStateManifestTransition.validate.
func ValidateManifestTransition(previous Manifest, previousCID string, candidate Manifest, candidateCID string) error {
	if previousCID == candidateCID {
		if !previous.Equal(candidate) {
			return ErrStaleGeneration
		}
		return nil
	}
	if candidate.Version < previous.Version || candidate.LastSequence < previous.LastSequence ||
		candidate.EffectiveRevision() <= previous.EffectiveRevision() {
		return ErrStaleGeneration
	}
	if candidate.Version == 1 && candidate.LastSequence == previous.LastSequence {
		return ErrStaleGeneration
	}
	return nil
}

// IsMaintenanceTransition reports a GC transaction that changes only the
// revision. Unchanged verified roots prove that neither executable state nor
// retry receipts changed.
func IsMaintenanceTransition(previous *Manifest, candidate Manifest) bool {
	if previous == nil || previous.Version != 2 || candidate.Version != 2 ||
		candidate.EffectiveRevision() <= previous.EffectiveRevision() {
		return false
	}
	return candidate.LastSequence == previous.LastSequence &&
		referencePtrEqual(candidate.StateHead, previous.StateHead) &&
		referencePtrEqual(candidate.DevicesHead, previous.DevicesHead) &&
		referencePtrEqual(candidate.LegacyReceiptsHead, previous.LegacyReceiptsHead) &&
		intPtrEqual(candidate.CompactionVersion, previous.CompactionVersion)
}

func referencePtrEqual(a, b *Reference) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
