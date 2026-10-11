package contracts

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed review-api.gen.json
var reviewAPI []byte

var reviewDefinitions = func() map[string]any {
	var document struct {
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(reviewAPI, &document); err != nil {
		panic("INVALID_EMBEDDED_REVIEW_SCHEMA")
	}
	return document.Components.Schemas
}()

// DecodeReview validates the shared schema before decoding a typed DTO.
func DecodeReview(name string, raw []byte, target any, maxBytes int) error {
	if len(raw) > maxBytes || !utf8.Valid(raw) {
		return errors.New("INVALID_REVIEW_BODY")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF || !reviewSchemaValid(reviewDefinitions[name], value) {
		return errors.New("INVALID_REVIEW_BODY")
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func reviewSchemaValid(definition, value any) bool {
	s, ok := definition.(map[string]any)
	if !ok {
		return false
	}
	if value == nil {
		return s["nullable"] == true
	}
	if name, ok := s["$ref"].(string); ok {
		return reviewSchemaValid(reviewDefinitions[strings.TrimPrefix(name, "#/components/schemas/")], value)
	}
	if options, ok := s["enum"].([]any); ok {
		found := false
		for _, option := range options {
			if fmtValue(option) == fmtValue(value) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	switch s["type"] {
	case "object":
		m, ok := value.(map[string]any)
		if !ok {
			return false
		}
		p, _ := s["properties"].(map[string]any)
		if required, ok := s["required"].([]any); ok {
			for _, key := range required {
				if _, exists := m[key.(string)]; !exists {
					return false
				}
			}
		}
		for key, item := range m {
			def, exists := p[key]
			if !exists {
				if s["additionalProperties"] == false {
					return false
				}
				continue
			}
			if !reviewSchemaValid(def, item) {
				return false
			}
		}
	case "array":
		a, ok := value.([]any)
		if !ok {
			return false
		}
		if max, ok := s["maxItems"].(float64); ok && len(a) > int(max) {
			return false
		}
		for _, item := range a {
			if !reviewSchemaValid(s["items"], item) {
				return false
			}
		}
	case "string":
		v, ok := value.(string)
		if !ok {
			return false
		}
		if max, ok := s["maxLength"].(float64); ok && utf8.RuneCountInString(v) > int(max) {
			return false
		}
		if pattern, ok := s["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(v) {
			return false
		}
		if s["format"] == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
				return false
			}
		}
		if s["pattern"] == "^(0|[1-9][0-9]{0,18})$" {
			if _, err := strconv.ParseInt(v, 10, 64); err != nil {
				return false
			}
		}
	case "integer":
		v, ok := value.(json.Number)
		if !ok {
			return false
		}
		n, err := v.Int64()
		if err != nil {
			return false
		}
		if min, ok := s["minimum"].(float64); ok && n < int64(min) {
			return false
		}
		if max, ok := s["maximum"].(float64); ok && n > int64(max) {
			return false
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return false
		}
	default:
		return false
	}
	return true
}

func fmtValue(value any) string { data, _ := json.Marshal(value); return string(data) }
