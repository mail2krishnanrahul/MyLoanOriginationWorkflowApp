package document

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var safeJSONPathSegment = regexp.MustCompile(`^[A-Za-z0-9_\-]+$`)

func normalizeExtension(ext string) string {
	ext = strings.TrimSpace(strings.ToLower(ext))
	ext = strings.TrimPrefix(ext, ".")
	return ext
}

func extensionFromFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return ""
	}
	lastDot := strings.LastIndex(filename, ".")
	if lastDot <= 0 || lastDot == len(filename)-1 {
		return ""
	}
	return normalizeExtension(filename[lastDot+1:])
}

func splitPath(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	segments := strings.Split(path, ".")
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		out = append(out, segment)
	}
	return out
}

func copyMap(input map[string]interface{}) (map[string]interface{}, error) {
	if input == nil {
		return map[string]interface{}{}, nil
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var copied map[string]interface{}
	if err := json.Unmarshal(raw, &copied); err != nil {
		return nil, err
	}
	if copied == nil {
		copied = map[string]interface{}{}
	}
	return copied, nil
}

func getByPath(data map[string]interface{}, path string) (interface{}, bool) {
	if len(data) == 0 {
		return nil, false
	}
	segments := splitPath(path)
	if len(segments) == 0 {
		return nil, false
	}

	var current interface{} = data
	for i, segment := range segments {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		value, exists := obj[segment]
		if !exists {
			return nil, false
		}
		if i == len(segments)-1 {
			return value, true
		}
		current = value
	}
	return nil, false
}

func setByPath(data map[string]interface{}, path string, value interface{}) bool {
	segments := splitPath(path)
	if len(segments) == 0 {
		return false
	}
	current := data
	for i := 0; i < len(segments)-1; i++ {
		segment := segments[i]
		next, exists := current[segment]
		if !exists {
			created := map[string]interface{}{}
			current[segment] = created
			current = created
			continue
		}
		nextMap, ok := next.(map[string]interface{})
		if !ok {
			created := map[string]interface{}{}
			current[segment] = created
			current = created
			continue
		}
		current = nextMap
	}
	current[segments[len(segments)-1]] = value
	return true
}

func deleteByPath(data map[string]interface{}, path string) bool {
	segments := splitPath(path)
	if len(segments) == 0 {
		return false
	}
	current := data
	for i := 0; i < len(segments)-1; i++ {
		next, ok := current[segments[i]]
		if !ok {
			return false
		}
		nextMap, ok := next.(map[string]interface{})
		if !ok {
			return false
		}
		current = nextMap
	}
	leaf := segments[len(segments)-1]
	if _, ok := current[leaf]; !ok {
		return false
	}
	delete(current, leaf)
	return true
}

func toJSONString(value interface{}) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(raw)
	}
}

func containsRole(allowed []string, role string) bool {
	role = strings.TrimSpace(strings.ToUpper(role))
	for _, candidate := range allowed {
		if strings.TrimSpace(strings.ToUpper(candidate)) == role {
			return true
		}
	}
	return false
}

func jsonbPathLiteral(path string) (string, error) {
	segments := splitPath(path)
	if len(segments) == 0 {
		return "", fmt.Errorf("jsonbPathLiteral: path is empty")
	}
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		if !safeJSONPathSegment.MatchString(segment) {
			return "", fmt.Errorf("jsonbPathLiteral: unsafe path segment %q", segment)
		}
		escaped = append(escaped, segment)
	}
	return fmt.Sprintf("'{%s}'", strings.Join(escaped, ",")), nil
}
