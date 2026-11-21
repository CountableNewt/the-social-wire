package atproto

import (
	"context"
	"strings"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

// LexiconMigrationSummary counts copied and deleted legacy records.
type LexiconMigrationSummary struct {
	FoldersCopied           int `json:"foldersCopied"`
	PublicationPrefsCopied  int `json:"publicationPrefsCopied"`
	PreferencesCopied       int `json:"preferencesCopied"`
	FoldersDeleted          int `json:"foldersDeleted"`
	PublicationPrefsDeleted int `json:"publicationPrefsDeleted"`
	PreferencesDeleted      int `json:"preferencesDeleted"`
}

// Changed reports whether any record was copied or deleted.
func (s LexiconMigrationSummary) Changed() bool {
	return s.FoldersCopied > 0 || s.PublicationPrefsCopied > 0 || s.PreferencesCopied > 0 ||
		s.FoldersDeleted > 0 || s.PublicationPrefsDeleted > 0 || s.PreferencesDeleted > 0
}

// MigrateLegacyLexiconsIfNeeded copies legacy com.thesocialwire.* records into
// app.thesocialwire.* (skipping records that already exist; preferences land
// at rkey "self") and deletes every legacy record.
func MigrateLegacyLexiconsIfNeeded(ctx context.Context, repo *RepoClient, auth *reqctx.Auth) (LexiconMigrationSummary, error) {
	var summary LexiconMigrationSummary
	for _, pair := range LegacyCollections() {
		probe, _, err := repo.ListRecords(ctx, auth, ListRecordsParams{Repo: auth.DID, Collection: pair.Legacy, Limit: 1})
		if err != nil {
			return summary, err
		}
		if len(probe) == 0 {
			continue
		}
		legacy, err := repo.ListAllRecords(ctx, auth, auth.DID, pair.Legacy, 0, 0)
		if err != nil {
			return summary, err
		}
		for _, record := range legacy {
			rkey := lastURISegment(record.URI)
			if rkey == "" {
				continue
			}
			existing, err := repo.GetRecord(ctx, auth, auth.DID, pair.Current, rkey)
			if err != nil {
				return summary, err
			}
			if existing == nil {
				migrated := make(map[string]any, len(record.Value)+1)
				for key, value := range record.Value {
					migrated[key] = value
				}
				migrated["$type"] = pair.Current
				switch pair.Legacy {
				case LegacyCollectionFolder:
					if err := repo.PutRecord(ctx, auth, pair.Current, rkey, migrated); err != nil {
						return summary, err
					}
					summary.FoldersCopied++
				case LegacyCollectionPublicationPrefs:
					if err := repo.PutRecord(ctx, auth, pair.Current, rkey, migrated); err != nil {
						return summary, err
					}
					summary.PublicationPrefsCopied++
				case LegacyCollectionPreferences:
					if err := repo.PutRecord(ctx, auth, pair.Current, PreferencesRKey, migrated); err != nil {
						return summary, err
					}
					summary.PreferencesCopied++
				}
			}
			if err := repo.DeleteRecord(ctx, auth, pair.Legacy, rkey); err != nil {
				return summary, err
			}
			switch pair.Legacy {
			case LegacyCollectionFolder:
				summary.FoldersDeleted++
			case LegacyCollectionPublicationPrefs:
				summary.PublicationPrefsDeleted++
			case LegacyCollectionPreferences:
				summary.PreferencesDeleted++
			}
		}
	}
	return summary, nil
}

// lastURISegment is `uri.split("/").last` (empty segments omitted).
func lastURISegment(uri string) string {
	parts := strings.FieldsFunc(uri, func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}
