// Package bean holds protocol-specific proxy configurations.
//
// A Bean is kept as a raw JSON object (map) instead of a typed struct so that
// every field written by the original nekoray is preserved on round-trip.
// Typed accessors are provided for the fields used by config building.
package bean

// Bean is a protocol-specific proxy configuration as a raw JSON object.
type Bean map[string]interface{}

// New returns an empty bean.
func New() Bean { return Bean{} }

// Have reports whether the key exists.
func (b Bean) Have(key string) bool {
	if b == nil {
		return false
	}
	_, ok := b[key]
	return ok
}

// Str returns the string value of key ("" if absent/not a string).
func (b Bean) Str(key string) string {
	if b == nil {
		return ""
	}
	if v, ok := b[key].(string); ok {
		return v
	}
	return ""
}

// Int returns the integer value of key (0 if absent/not numeric).
func (b Bean) Int(key string) int {
	if b == nil {
		return 0
	}
	switch v := b[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// Int64 returns the int64 value of key.
func (b Bean) Int64(key string) int64 {
	if b == nil {
		return 0
	}
	switch v := b[key].(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

// Bool returns the boolean value of key.
func (b Bean) Bool(key string) bool {
	if b == nil {
		return false
	}
	if v, ok := b[key].(bool); ok {
		return v
	}
	return false
}

// IntList returns key as a list of ints.
func (b Bean) IntList(key string) []int {
	if b == nil {
		return nil
	}
	arr, ok := b[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]int, 0, len(arr))
	for _, v := range arr {
		switch n := v.(type) {
		case float64:
			out = append(out, int(n))
		case int:
			out = append(out, n)
		}
	}
	return out
}

// StrList returns key as a list of strings.
func (b Bean) StrList(key string) []string {
	if b == nil {
		return nil
	}
	arr, ok := b[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Sub returns the nested object at key as a Bean. If missing it is created.
func (b Bean) Sub(key string) Bean {
	if b == nil {
		return nil
	}
	switch v := b[key].(type) {
	case map[string]interface{}:
		return Bean(v)
	case Bean:
		return v
	}
	nb := Bean{}
	b[key] = map[string]interface{}(nb)
	return nb
}

// Peek returns the nested object at key, or nil when absent.
func (b Bean) Peek(key string) Bean {
	if b == nil {
		return nil
	}
	switch v := b[key].(type) {
	case map[string]interface{}:
		return Bean(v)
	case Bean:
		return v
	}
	return nil
}

// Set assigns a value.
func (b Bean) Set(key string, val interface{}) {
	if b != nil {
		b[key] = val
	}
}
