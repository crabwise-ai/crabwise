package classify

import (
	"encoding/json"
	"sort"
	"strings"
)

func ExtractArgKeys(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var payload interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	keys := make(map[string]struct{})
	collectArgKeys(payload, keys)

	return sortUniqueKeys(keys)
}

func NormalizeArgKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}

	normalized := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = normalizeArgKey(key)
		if key == "" {
			continue
		}
		normalized[key] = struct{}{}
	}

	return sortUniqueKeys(normalized)
}

func collectArgKeys(v interface{}, out map[string]struct{}) {
	switch val := v.(type) {
	case map[string]interface{}:
		for key, child := range val {
			normalized := normalizeArgKey(key)
			if normalized != "" {
				out[normalized] = struct{}{}
			}
			collectArgKeys(child, out)
		}
	case []interface{}:
		for _, child := range val {
			collectArgKeys(child, out)
		}
	}
}

func sortUniqueKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func normalizeArgKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}
