package sub

import (
	"encoding/json"

	"eagle/internal/bean"
	"eagle/internal/db"
)

// BeanKey returns the dedup key of a profile, mirroring
// nekoray's ProfileFilter_ent_key. Empty strings are dropped and c_cfg/c_out
// are ignored so profiles loaded from disk compare equal to freshly parsed ones.
func BeanKey(ent *db.ProxyEntity, byAddress bool) string {
	if byAddress && ent.Type != "custom" {
		return ent.DisplayAddress() + ent.DisplayType()
	}
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

// OnlyInSrc returns src entries whose key is not present in dst.
func OnlyInSrc(src, dst []*db.ProxyEntity) []*db.ProxyEntity {
	keys := map[string]bool{}
	for _, e := range dst {
		keys[BeanKey(e, false)] = true
	}
	var out []*db.ProxyEntity
	for _, e := range src {
		if !keys[BeanKey(e, false)] {
			out = append(out, e)
		}
	}
	return out
}

// OnlyInSrcByPointer returns src entries not present in dst by pointer identity.
func OnlyInSrcByPointer(src, dst []*db.ProxyEntity) []*db.ProxyEntity {
	var out []*db.ProxyEntity
	for _, e := range src {
		found := false
		for _, d := range dst {
			if d == e {
				found = true
				break
			}
		}
		if !found {
			out = append(out, e)
		}
	}
	return out
}

// Common returns entries matched by key across src and dst as two parallel
// slices (keep from src, del from dst).
func Common(src, dst []*db.ProxyEntity) (keep, del []*db.ProxyEntity) {
	hash := map[string]*db.ProxyEntity{}
	for _, e := range src {
		hash[BeanKey(e, false)] = e
	}
	for _, e := range dst {
		if k, ok := hash[BeanKey(e, false)]; ok {
			del = append(del, e)
			keep = append(keep, k)
		}
	}
	return keep, del
}
