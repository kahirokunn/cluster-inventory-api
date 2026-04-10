/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package clusterprofile

import (
	"sort"

	v1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

// OwnedProperties is the desired set of properties that *this* writer owns.
// Keys are property names; values are property values.
// Delete is represented by absence: if a previously-owned name is missing
// from OwnedProperties, SSA will remove it server-side.
type OwnedProperties map[string]string

// BuildApplyList renders OwnedProperties into a deterministic (sorted by name)
// list of Property entries. The output contains exactly the entries in owned —
// nothing more, nothing less. This is the core multi-writer safety invariant:
// the SSA payload must include only fields this writer owns.
func BuildApplyList(owned OwnedProperties) []PropertyItem {
	if len(owned) == 0 {
		return nil
	}

	names := make([]string, 0, len(owned))
	for n := range owned {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]PropertyItem, len(names))
	for i, n := range names {
		out[i] = PropertyItem{Name: n, Value: owned[n]}
	}
	return out
}

// PropertyItem represents a single entry in ClusterProfile status.properties
// for SSA payloads.
type PropertyItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// BuildApplyPayload converts owned properties into the map[string]any shape
// expected by the SSA status patch. The returned map has a single key
// "properties" containing the sorted list of owned entries.
func BuildApplyPayload(owned OwnedProperties) map[string]any {
	list := BuildApplyList(owned)
	if list == nil {
		return map[string]any{"properties": []any{}}
	}

	items := make([]any, len(list))
	for i, p := range list {
		items[i] = map[string]any{
			"name":  p.Name,
			"value": p.Value,
		}
	}
	return map[string]any{"properties": items}
}

// GetStatusProperty reads a single property value from a typed ClusterProfile's
// status.properties list. Returns the value and true if found, or empty string
// and false if the property does not exist.
func GetStatusProperty(cp *v1alpha1.ClusterProfile, propertyName string) (string, bool) {
	for _, p := range cp.Status.Properties {
		if p.Name == propertyName {
			return p.Value, true
		}
	}
	return "", false
}
