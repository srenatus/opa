// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"encoding/json"
	"fmt"
)

// serializableTreeNode is a JSON-serializable representation of TreeNode
type serializableTreeNode struct {
	Key      json.RawMessage         `json:"key"`
	Values   []json.RawMessage       `json:"values"`
	Children []serializableTreeChild `json:"children,omitempty"`
	Sorted   []json.RawMessage       `json:"sorted,omitempty"`
	Hide     bool                    `json:"hide"`
}

type serializableTreeChild struct {
	Key  json.RawMessage       `json:"key"`
	Node *serializableTreeNode `json:"node"`
}

// MarshalJSON implements json.Marshaler for TreeNode
func (n *TreeNode) MarshalJSON() ([]byte, error) {
	if n == nil {
		return []byte("null"), nil
	}

	sn := serializableTreeNode{
		Hide: n.Hide,
	}

	// Serialize key
	if n.Key != nil {
		keyBytes, err := json.Marshal(n.Key)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal TreeNode key: %w", err)
		}
		sn.Key = keyBytes
	}

	// Serialize values (Rules)
	if len(n.Values) > 0 {
		sn.Values = make([]json.RawMessage, len(n.Values))
		for i, v := range n.Values {
			vBytes, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal TreeNode value %d: %w", i, err)
			}
			sn.Values[i] = vBytes
		}
	}

	// Serialize sorted keys
	if len(n.Sorted) > 0 {
		sn.Sorted = make([]json.RawMessage, len(n.Sorted))
		for i, v := range n.Sorted {
			vBytes, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal TreeNode sorted key %d: %w", i, err)
			}
			sn.Sorted[i] = vBytes
		}
	}

	// Serialize children map as slice
	if len(n.Children) > 0 {
		sn.Children = make([]serializableTreeChild, 0, len(n.Children))
		for k, child := range n.Children {
			keyBytes, err := json.Marshal(k)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal TreeNode child key: %w", err)
			}

			childNode, err := child.MarshalJSON()
			if err != nil {
				return nil, fmt.Errorf("failed to marshal TreeNode child: %w", err)
			}

			var childSN serializableTreeNode
			if err := json.Unmarshal(childNode, &childSN); err != nil {
				return nil, fmt.Errorf("failed to unmarshal child node: %w", err)
			}

			sn.Children = append(sn.Children, serializableTreeChild{
				Key:  keyBytes,
				Node: &childSN,
			})
		}
	}

	return json.Marshal(sn)
}

// UnmarshalJSON implements json.Unmarshaler for TreeNode
func (n *TreeNode) UnmarshalJSON(data []byte) error {
	var sn serializableTreeNode
	if err := json.Unmarshal(data, &sn); err != nil {
		return fmt.Errorf("failed to unmarshal TreeNode: %w", err)
	}

	n.Hide = sn.Hide

	// Unmarshal key
	if len(sn.Key) > 0 {
		var key Value
		if err := json.Unmarshal(sn.Key, &key); err != nil {
			return fmt.Errorf("failed to unmarshal TreeNode key: %w", err)
		}
		n.Key = key
	}

	// Unmarshal values (Rules)
	if len(sn.Values) > 0 {
		n.Values = make([]any, len(sn.Values))
		for i, vBytes := range sn.Values {
			var rule Rule
			if err := json.Unmarshal(vBytes, &rule); err != nil {
				return fmt.Errorf("failed to unmarshal TreeNode value %d: %w", i, err)
			}
			n.Values[i] = &rule
		}
	}

	// Unmarshal sorted keys
	if len(sn.Sorted) > 0 {
		n.Sorted = make([]Value, len(sn.Sorted))
		for i, vBytes := range sn.Sorted {
			var val Value
			if err := json.Unmarshal(vBytes, &val); err != nil {
				return fmt.Errorf("failed to unmarshal TreeNode sorted key %d: %w", i, err)
			}
			n.Sorted[i] = val
		}
	}

	// Unmarshal children
	if len(sn.Children) > 0 {
		n.Children = make(map[Value]*TreeNode, len(sn.Children))
		for _, child := range sn.Children {
			var key Value
			if err := json.Unmarshal(child.Key, &key); err != nil {
				return fmt.Errorf("failed to unmarshal TreeNode child key: %w", err)
			}

			childBytes, err := json.Marshal(child.Node)
			if err != nil {
				return fmt.Errorf("failed to marshal child node for unmarshal: %w", err)
			}

			var childNode TreeNode
			if err := json.Unmarshal(childBytes, &childNode); err != nil {
				return fmt.Errorf("failed to unmarshal TreeNode child: %w", err)
			}

			n.Children[key] = &childNode
		}
	}

	return nil
}
