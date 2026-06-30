package projector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"talent-forge/types"
)

type FieldSpec struct {
	Path      string `json:"path"`
	From      string `json:"from,omitempty"`
	Type      string `json:"type,omitempty"`
	Required  bool   `json:"required,omitempty"`
	Normalize string `json:"normalize,omitempty"`
}

type Config struct {
	Fields            []FieldSpec `json:"fields"`
	IncludeConfidence bool        `json:"include_confidence"`
	OnMissing         string      `json:"on_missing"`
}

type ValidationError struct {
	Failures []string
}

func (v *ValidationError) Error() string {
	return "projector: validation failed: " + strings.Join(v.Failures, "; ")
}

const (
	OnMissingNull  = "null"
	OnMissingOmit  = "omit"
	OnMissingError = "error"
)

func ParseConfig(raw []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("projector: invalid config: %w", err)
	}
	if cfg.OnMissing == "" {
		cfg.OnMissing = OnMissingNull
	}
	switch cfg.OnMissing {
	case OnMissingNull, OnMissingOmit, OnMissingError:
	default:
		return nil, fmt.Errorf("projector: invalid on_missing %q (want null|omit|error)", cfg.OnMissing)
	}
	for i, f := range cfg.Fields {
		if f.Path == "" {
			return nil, fmt.Errorf("projector: fields[%d].path is required", i)
		}
	}
	return &cfg, nil
}

func Project(profile *types.CanonicalProfile, cfg *Config) ([]byte, error) {
	root, err := profileToMap(profile)
	if err != nil {
		return nil, err
	}

	out := make(map[string]interface{}, len(cfg.Fields))
	order := make([]string, 0, len(cfg.Fields))
	var failures []string

	for _, f := range cfg.Fields {
		from := f.From
		if from == "" {
			from = f.Path
		}

		value, found := resolvePath(root, from)
		missing := !found || isEmpty(value)

		if missing {
			if f.Required {
				failures = append(failures, fmt.Sprintf("required field %q missing", f.Path))
				continue
			}
			switch cfg.OnMissing {
			case OnMissingOmit:
				continue
			case OnMissingError:
				failures = append(failures, fmt.Sprintf("field %q missing (on_missing=error)", f.Path))
				continue
			case OnMissingNull:
				if cfg.IncludeConfidence {
					out[f.Path] = map[string]interface{}{"value": nil, "confidence": 0.0}
				} else {
					out[f.Path] = nil
				}
				order = append(order, f.Path)
				continue
			}
		}

		if f.Normalize != "" {
			n, err := normalize(value, f.Normalize)
			if err != nil {
				failures = append(failures, fmt.Sprintf("field %q normalize=%s: %v", f.Path, f.Normalize, err))
				continue
			}
			value = n
		}

		if f.Type != "" {
			if err := validateType(value, f.Type); err != nil {
				failures = append(failures, fmt.Sprintf("field %q: %v", f.Path, err))
				continue
			}
		}

		if cfg.IncludeConfidence {
			conf := lookupConfidence(profile, f, from)
			out[f.Path] = map[string]interface{}{"value": value, "confidence": conf}
		} else {
			out[f.Path] = value
		}
		order = append(order, f.Path)
	}

	if len(failures) > 0 {
		return nil, &ValidationError{Failures: failures}
	}

	return marshalOrdered(out, order)
}

func profileToMap(profile *types.CanonicalProfile) (map[string]interface{}, error) {
	data, err := json.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("projector: marshal profile: %w", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("projector: unmarshal profile: %w", err)
	}
	return m, nil
}

var pathSegRe = regexp.MustCompile(`^([^.\[\]]+)(\[\d*\])?$`)

func resolvePath(root interface{}, path string) (interface{}, bool) {
	if path == "" {
		return root, true
	}
	segments := strings.Split(path, ".")
	var cur interface{} = root
	for _, seg := range segments {
		m := pathSegRe.FindStringSubmatch(seg)
		if m == nil {
			return nil, false
		}
		key := m[1]
		idx := m[2]

		mp, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		next, exists := mp[key]
		if !exists {
			return nil, false
		}

		if idx == "" {
			cur = next
			continue
		}
		if idx == "[]" {
			arr, ok := next.([]interface{})
			if !ok {
				return nil, false
			}
			cur = arr
			continue
		}
		i, err := strconv.Atoi(idx[1 : len(idx)-1])
		if err != nil {
			return nil, false
		}
		arr, ok := next.([]interface{})
		if !ok {
			return nil, false
		}
		if i < 0 || i >= len(arr) {
			return nil, false
		}
		cur = arr[i]
	}

	if strings.Contains(path, "[].") {
		cur = projectArraySelector(root, path)
	}
	return cur, cur != nil
}

func projectArraySelector(root interface{}, path string) interface{} {
	idx := strings.Index(path, "[]")
	if idx == -1 {
		return nil
	}
	prefix := path[:idx]
	rest := strings.TrimPrefix(path[idx+2:], ".")
	arrVal, ok := resolvePath(root, prefix)
	if !ok {
		return nil
	}
	arr, ok := arrVal.([]interface{})
	if !ok {
		return nil
	}
	out := make([]interface{}, 0, len(arr))
	for _, item := range arr {
		if rest == "" {
			out = append(out, item)
			continue
		}
		v, found := resolvePath(item, rest)
		if found {
			out = append(out, v)
		}
	}
	return out
}

func isEmpty(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.Len() == 0
	case reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return true
		}
		return isEmpty(rv.Elem().Interface())
	}
	return false
}

func validateType(v interface{}, t string) error {
	switch t {
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("expected string, got %T", v)
		}
	case "number":
		switch v.(type) {
		case float64, float32, int, int64:
		default:
			return fmt.Errorf("expected number, got %T", v)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", v)
		}
	case "string[]":
		arr, ok := v.([]interface{})
		if !ok {
			return fmt.Errorf("expected string[], got %T", v)
		}
		for i, item := range arr {
			if _, ok := item.(string); !ok {
				return fmt.Errorf("expected string at index %d, got %T", i, item)
			}
		}
	case "object":
		if _, ok := v.(map[string]interface{}); !ok {
			return fmt.Errorf("expected object, got %T", v)
		}
	case "object[]":
		arr, ok := v.([]interface{})
		if !ok {
			return fmt.Errorf("expected object[], got %T", v)
		}
		for i, item := range arr {
			if _, ok := item.(map[string]interface{}); !ok {
				return fmt.Errorf("expected object at index %d, got %T", i, item)
			}
		}
	case "":
		return nil
	default:
		return fmt.Errorf("unknown type %q", t)
	}
	return nil
}

func normalize(v interface{}, kind string) (interface{}, error) {
	switch kind {
	case "E164":
		switch x := v.(type) {
		case string:
			return toE164(x), nil
		case []interface{}:
			out := make([]interface{}, len(x))
			for i, item := range x {
				s, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("E164 expects string elements, got %T", item)
				}
				out[i] = toE164(s)
			}
			return out, nil
		default:
			return nil, fmt.Errorf("E164 expects string or string[], got %T", v)
		}
	case "canonical":
		switch x := v.(type) {
		case string:
			return canonicalSkill(x), nil
		case []interface{}:
			out := make([]interface{}, len(x))
			for i, item := range x {
				s, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("canonical expects string elements, got %T", item)
				}
				out[i] = canonicalSkill(s)
			}
			return out, nil
		default:
			return nil, fmt.Errorf("canonical expects string or string[], got %T", v)
		}
	}
	return nil, fmt.Errorf("unknown normalize kind %q", kind)
}

var nonDigit = regexp.MustCompile(`\D`)

func toE164(raw string) string {
	if raw == "" {
		return ""
	}
	digits := nonDigit.ReplaceAllString(raw, "")
	if digits == "" {
		return ""
	}
	if strings.HasPrefix(raw, "+") {
		return "+" + digits
	}
	if len(digits) == 10 {
		return "+1" + digits
	}
	return "+" + digits
}

func canonicalSkill(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func lookupConfidence(profile *types.CanonicalProfile, f FieldSpec, from string) float64 {
	if profile == nil || profile.FieldConfidence == nil {
		return 0
	}
	if c, ok := profile.FieldConfidence[from]; ok {
		return c
	}
	root := strings.Split(from, ".")[0]
	root = strings.TrimSuffix(strings.Split(root, "[")[0], "]")
	if c, ok := profile.FieldConfidence[root]; ok {
		return c
	}
	if c, ok := profile.FieldConfidence[f.Path]; ok {
		return c
	}
	return 0
}

func marshalOrdered(m map[string]interface{}, order []string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range order {
		buf.WriteString("  ")
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteString(": ")
		vb, err := json.MarshalIndent(m[k], "  ", "  ")
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
		if i < len(order)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}")
	return buf.Bytes(), nil
}
