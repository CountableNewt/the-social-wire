package readstate

import (
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // calendar time zones validate without host zoneinfo

	"github.com/CountableNewt/the-social-wire/services/appview/internal/isotime"
)

// Record limits shared with packages/read-state.
const (
	MaximumRecordBytes       = 65_536
	MaximumSequence    int64 = 9_007_199_254_740_991
)

var referenceDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ParseDate is ReadStateValidation.date: strict RFC 3339 that keeps
// microsecond fractions so URI tie boundaries do not move.
func ParseDate(value string) (time.Time, error) {
	parsed, err := isotime.Parse(value)
	if err != nil {
		return time.Time{}, ErrInvalidRecord
	}
	return parsed, nil
}

// ValidateReference requires a chunk URI in the viewer's own repository.
func ValidateReference(reference Reference, viewerDID string) error {
	prefix := "at://" + viewerDID + "/" + ChunkCollection + "/"
	if !strings.HasPrefix(viewerDID, "did:") || !strings.HasPrefix(reference.URI, prefix) ||
		reference.CID == "" || len(reference.CID) > 256 {
		return ErrInvalidReference
	}
	key := reference.URI[len(prefix):]
	if key == "" || key == "." || key == ".." || len(key) > 512 {
		return ErrInvalidReference
	}
	for index := 0; index < len(key); index++ {
		c := key[index]
		if !(c >= '0' && c <= '9') && !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') &&
			c != '-' && c != '.' && c != ':' && c != '_' && c != '~' {
			return ErrInvalidReference
		}
	}
	return nil
}

// ValidateManifest checks a v1 or v2 manifest against the viewer.
func ValidateManifest(manifest Manifest, viewerDID string) error {
	if manifest.Type != ManifestCollection || (manifest.Version != 1 && manifest.Version != 2) ||
		manifest.Generation == "" || len(manifest.Generation) > 128 ||
		manifest.LastSequence < 0 || manifest.LastSequence > MaximumSequence {
		return ErrInvalidRecord
	}
	if manifest.Version == 1 {
		if (manifest.Head == nil && manifest.LastSequence != 0) || manifest.Revision != nil ||
			manifest.StateHead != nil || manifest.DevicesHead != nil || manifest.LegacyReceiptsHead != nil ||
			manifest.CompactionVersion != nil {
			return ErrInvalidRecord
		}
		if manifest.Head != nil {
			if err := ValidateReference(*manifest.Head, viewerDID); err != nil {
				return err
			}
		}
	} else {
		if manifest.Revision == nil || *manifest.Revision < 1 || *manifest.Revision > MaximumSequence ||
			manifest.Head != nil || manifest.CompactionVersion == nil || *manifest.CompactionVersion != 1 ||
			len(manifest.Extensions) != 0 || (manifest.LastSequence != 0 && manifest.StateHead == nil) {
			return ErrInvalidRecord
		}
		for _, reference := range []*Reference{manifest.StateHead, manifest.DevicesHead, manifest.LegacyReceiptsHead} {
			if reference != nil {
				if err := ValidateReference(*reference, viewerDID); err != nil {
					return err
				}
			}
		}
	}
	return ValidateSize(manifest)
}

// ValidateChunk checks a v1 chunk against the viewer.
func ValidateChunk(chunk Chunk, viewerDID string) error {
	if chunk.Type != ChunkCollection || chunk.Version != 1 || len(chunk.Operations) == 0 ||
		len(chunk.Operations) > 128 {
		return ErrInvalidRecord
	}
	for _, operation := range chunk.Operations {
		if err := ValidateOperation(operation); err != nil {
			return err
		}
	}
	if chunk.Previous != nil {
		if err := ValidateReference(*chunk.Previous, viewerDID); err != nil {
			return err
		}
	}
	return ValidateSize(chunk)
}

// ValidateOperation checks one operation's fields and selection.
func ValidateOperation(operation Operation) error {
	if operation.ActionID == "" || len(operation.ActionID) > 128 ||
		operation.Sequence < 1 || operation.Sequence > MaximumSequence {
		return ErrInvalidRecord
	}
	if _, err := ParseDate(operation.ActedAt); err != nil {
		return err
	}
	if calendar := operation.Calendar; calendar != nil {
		if operation.Selection != SelectionExact || !knownTimeZone(calendar.TimeZone) ||
			!referenceDatePattern.MatchString(calendar.ReferenceDate) {
			return ErrInvalidRecord
		}
		if _, err := ParseDate(calendar.Cutoff); err != nil {
			return err
		}
		if _, err := ParseDate(calendar.ReferenceDate + "T00:00:00Z"); err != nil {
			return err
		}
	}
	switch operation.Selection {
	case SelectionExact:
		subjects := operation.SubjectURIs
		if operation.Boundaries != nil || subjects == nil || len(subjects) == 0 || len(subjects) > 256 {
			return ErrInvalidRecord
		}
		seen := make(map[string]struct{}, len(subjects))
		for _, subject := range subjects {
			if subject == "" || len(subject) > 2048 {
				return ErrInvalidRecord
			}
			if _, duplicate := seen[subject]; duplicate {
				return ErrInvalidRecord
			}
			seen[subject] = struct{}{}
		}
	case SelectionBoundaries:
		boundaries := operation.Boundaries
		if operation.SubjectURIs != nil || boundaries == nil || len(boundaries) == 0 || len(boundaries) > 128 {
			return ErrInvalidRecord
		}
		for _, boundary := range boundaries {
			scope := boundary.Scope
			if scope.PublicationID == "" || len(scope.PublicationID) > 2048 ||
				!strings.HasPrefix(scope.AuthorDID, "did:") || len(scope.AuthorDID) > 2048 ||
				len(scope.PublicationSiteKeys) > 128 {
				return ErrInvalidRecord
			}
			for _, key := range scope.PublicationSiteKeys {
				if key == "" || len(key) > 2048 {
					return ErrInvalidRecord
				}
			}
			if boundary.EntryID != nil && (*boundary.EntryID == "" || len(*boundary.EntryID) > 2048) {
				return ErrInvalidRecord
			}
			if _, err := ParseDate(boundary.CreatedAt); err != nil {
				return err
			}
		}
	default:
		return ErrInvalidRecord
	}
	return nil
}

// knownTimeZone approximates TimeZone.knownTimeZoneIdentifiers with the
// embedded IANA database.
func knownTimeZone(identifier string) bool {
	if identifier == "UTC" {
		return true
	}
	if identifier == "" || identifier == "Local" {
		return false
	}
	_, err := time.LoadLocation(identifier)
	return err == nil
}

// EncodedByteCount is the JSON size with unescaped slashes, the size every
// client and the PDS limit apply.
func EncodedByteCount(record any) (int, error) {
	data, err := marshalNoEscape(record)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

// ValidateSize rejects records over MaximumRecordBytes.
func ValidateSize(record any) error {
	count, err := EncodedByteCount(record)
	if err != nil {
		return err
	}
	if count > MaximumRecordBytes {
		return ErrSizeLimit
	}
	return nil
}
