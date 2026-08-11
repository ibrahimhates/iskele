package service

import "encoding/json"

// marshalMerged encodes base and splices extra fields in at the top level.
//
// It exists for the types that embed a store record and add live state on top:
// an embedded type's MarshalJSON is promoted to the outer struct, so the
// default encoding of such a type is the record alone — silently dropping
// everything the outer type added. Both callers here were bitten by that once.
func marshalMerged(base any, extra map[string]any) ([]byte, error) {
	encoded, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}

	merged := map[string]any{}
	if err := json.Unmarshal(encoded, &merged); err != nil {
		return nil, err
	}

	for key, value := range extra {
		merged[key] = value
	}
	return json.Marshal(merged)
}
