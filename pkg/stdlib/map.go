package stdlib

import (
	"fmt"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// registerMapFuncs registers map.* functions.
func (r *Registry) registerMapFuncs() {
	r.Register("map.get", mapGet)
	r.Register("map.delete", mapDelete)
	r.Register("map.merge", mapMerge)
	r.Register("map.merge_nested", mapMergeNested)
}

func mapGet(args []types.Value) (types.Value, error) {
	// map.get(map, key) or map.get(map, key, default)
	// Can be called positionally from expressions or via map-style args from call steps

	var m types.Value
	var key string
	var defaultVal types.Value = types.Null
	hasDefault := false

	if len(args) == 1 && args[0].Type() == types.TypeMap {
		// Map-style args from call step: {map: ..., key: ..., default: ...}
		am := args[0].AsMap()
		if mv, ok := am.Get("map"); ok {
			m = mv
			if kv, ok := am.Get("key"); ok {
				key = kv.AsString()
			}
			if dv, ok := am.Get("default"); ok {
				defaultVal = dv
				hasDefault = true
			}
		} else {
			// It's a positional map arg (the map itself)
			m = args[0]
			return types.Null, fmt.Errorf("map.get requires map and key arguments")
		}
	} else if len(args) >= 2 {
		// Positional: map.get(m, "key") or map.get(m, "key", default)
		m = args[0]
		if args[1].Type() == types.TypeString {
			key = args[1].AsString()
		} else {
			return types.Null, types.NewTypeError("map.get: key must be a string")
		}
		if len(args) >= 3 {
			defaultVal = args[2]
			hasDefault = true
		}
	} else {
		return types.Null, fmt.Errorf("map.get requires map and key arguments")
	}

	if m.Type() != types.TypeMap {
		return types.Null, types.NewTypeError("map.get: first argument must be a map")
	}

	val, ok := m.AsMap().Get(key)
	if !ok {
		if hasDefault {
			return defaultVal, nil
		}
		return types.Null, nil
	}
	return val, nil
}

func mapDelete(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("map.delete requires arguments")
	}

	var m types.Value
	var key string

	if args[0].Type() == types.TypeMap {
		if _, ok := args[0].AsMap().Get("map"); ok {
			am := args[0].AsMap()
			if mv, ok := am.Get("map"); ok {
				m = mv
			}
			if kv, ok := am.Get("key"); ok {
				key = kv.AsString()
			}
		} else if len(args) >= 2 {
			m = args[0]
			key = args[1].AsString()
		}
	}

	if m.Type() != types.TypeMap {
		return types.Null, types.NewTypeError("map.delete: first argument must be a map")
	}

	// Return a new map without the key
	result := types.NewOrderedMap()
	for _, k := range m.AsMap().Keys() {
		if k != key {
			v, _ := m.AsMap().Get(k)
			result.Set(k, v)
		}
	}
	return types.NewMap(result), nil
}

func mapMerge(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("map.merge requires arguments")
	}

	var maps []types.Value

	if args[0].Type() == types.TypeMap {
		if objs, ok := args[0].AsMap().Get("objs"); ok && objs.Type() == types.TypeList {
			maps = objs.AsList()
		} else {
			// Positional: merge all map args
			maps = args
		}
	} else {
		maps = args
	}

	result := types.NewOrderedMap()
	for _, m := range maps {
		if m.Type() != types.TypeMap {
			return types.Null, types.NewTypeError("map.merge: all arguments must be maps")
		}
		for _, k := range m.AsMap().Keys() {
			v, _ := m.AsMap().Get(k)
			result.Set(k, v)
		}
	}

	return types.NewMap(result), nil
}

func mapMergeNested(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("map.merge_nested requires arguments")
	}

	var maps []types.Value

	if args[0].Type() == types.TypeMap {
		if objs, ok := args[0].AsMap().Get("objs"); ok && objs.Type() == types.TypeList {
			maps = objs.AsList()
		} else {
			maps = args
		}
	} else {
		maps = args
	}

	if len(maps) == 0 {
		return types.NewMap(types.NewOrderedMap()), nil
	}

	result := maps[0].Clone()
	for i := 1; i < len(maps); i++ {
		result = deepMerge(result, maps[i])
	}

	return result, nil
}

// deepMerge recursively merges two maps.
func deepMerge(base, overlay types.Value) types.Value {
	if base.Type() != types.TypeMap || overlay.Type() != types.TypeMap {
		return overlay
	}

	result := base.Clone().AsMap()
	for _, k := range overlay.AsMap().Keys() {
		ov, _ := overlay.AsMap().Get(k)
		if existing, ok := result.Get(k); ok {
			result.Set(k, deepMerge(existing, ov))
		} else {
			result.Set(k, ov)
		}
	}

	return types.NewMap(result)
}
