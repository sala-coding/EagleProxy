package sub

import (
	"encoding/json"

	"eagle/internal/bean"
	"eagle/internal/db"
)

// BeanKey returns the dedup key of a profile, mirroring
// nekoray's ProfileFilter_ent_key. Empty strings are dropped and c_cfg/c_out
// are ignored so profiles loaded from disk compare equal to freshly parsed ones.
func BeanKey(ent *db.ProxyEntity) string {
	return canonicalBean(ent.Bean) + ent.DisplayType()
}

func canonicalBean(b bean.Bean) string {
	norm := normalize(map[string]interface{}(b), true)
	data, _ := json.Marshal(norm)
	return string(data)
}

func normalize(v interface{}, top bool) interface{} {
	switch t := v.(type) {
	case bean.Bean:
		return normalize(map[string]interface{}(t), top)
	case map[string]interface{}:
		out := map[string]interface{}{}
		for k, val := range t {
			if top && (k == "c_cfg" || k == "c_out") {
				continue
			}
			if s, ok := val.(string); ok && s == "" {
				continue
			}
			out[k] = normalize(val, false)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, x := range t {
			out[i] = normalize(x, false)
		}
		return out
	default:
		return v
	}
}
