package thinstore

import "encoding/json"

// Recognized author-declared content labels; labels never change feed membership.
var supportedContentLabels = []string{"ai-generated", "sexual"}

// ContentLabelsFromRecord reads self labels from a site.standard.document record.
func ContentLabelsFromRecord(record map[string]any) []string {
	if recordType, _ := record["$type"].(string); recordType != "site.standard.document" {
		return nil
	}
	labels, ok := record["labels"].(map[string]any)
	if !ok {
		return nil
	}
	if labelType, _ := labels["$type"].(string); labelType != "com.atproto.label.defs#selfLabels" {
		return nil
	}
	values, ok := labels["values"].([]any)
	if !ok {
		return nil
	}
	var names []string
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			// [[String: Any]] casting fails when any element is not an object.
			return nil
		}
		if name, ok := object["val"].(string); ok {
			names = append(names, name)
		}
	}
	return normalizeContentLabels(names)
}

// decodeContentLabelsJSON decodes a stored contentLabels array.
func decodeContentLabelsJSON(raw *string) []string {
	if raw == nil {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(*raw), &values); err != nil {
		return nil
	}
	return normalizeContentLabels(values)
}

func normalizeContentLabels(values []string) []string {
	present := map[string]bool{}
	for _, value := range values {
		present[value] = true
	}
	var normalized []string
	for _, label := range supportedContentLabels {
		if present[label] {
			normalized = append(normalized, label)
		}
	}
	return normalized
}
