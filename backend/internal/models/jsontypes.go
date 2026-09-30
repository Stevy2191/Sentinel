package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// ConditionSet is a port's active conditions, stored as a JSON array. Unlike
// StringSlice (where NULL means "every channel"), nil is stored as [] so the
// NOT NULL column never receives NULL.
type ConditionSet []string

func (s ConditionSet) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	return json.Marshal(s)
}

func (s *ConditionSet) Scan(value any) error {
	if value == nil {
		*s = ConditionSet{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning ConditionSet: %w", err)
	}
	return json.Unmarshal(data, s)
}

// TimeMap maps a key (a condition) to a time, stored as a JSON object; nil is
// stored as {}.
type TimeMap map[string]time.Time

func (m TimeMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
}

func (m *TimeMap) Scan(value any) error {
	if value == nil {
		*m = TimeMap{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning TimeMap: %w", err)
	}
	return json.Unmarshal(data, m)
}

// IntSlice is a nullable JSON array of integers: nil is SQL NULL (e.g. "no
// override").
type IntSlice []int

func (s IntSlice) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

func (s *IntSlice) Scan(value any) error {
	if value == nil {
		*s = nil
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning IntSlice: %w", err)
	}
	return json.Unmarshal(data, s)
}

// JSONMap is a free-form JSON object; nil is stored as {}.
type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(value any) error {
	if value == nil {
		*m = JSONMap{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning JSONMap: %w", err)
	}
	return json.Unmarshal(data, m)
}

// Opt is a PATCH field: Set is false when the key was absent, and Value is nil
// when it was JSON null ("clear") and non-nil when it held a value.
type Opt[T any] struct {
	Set   bool
	Value *T
}

func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}
