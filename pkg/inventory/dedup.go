/*
Copyright 2026 The Kubernetes Authors.

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

// Package inventory provides helpers for consuming ClusterProfile objects in a
// cluster inventory.
package inventory

import (
	"sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

// ClusterID returns the value of the id.k8s.io property of the given
// ClusterProfile and whether the property is present.
func ClusterID(profile *v1alpha1.ClusterProfile) (string, bool) {
	for _, property := range profile.Status.Properties {
		if property.Name == v1alpha1.PropertyClusterID {
			return property.Value, true
		}
	}
	return "", false
}

// Deduplicate returns profiles without duplicate representations of the same
// member cluster. ClusterProfile objects in the same namespace with the same
// id.k8s.io property represent the same member cluster; only the object with
// the oldest creationTimestamp, breaking ties by lexicographic order of name,
// is returned. Profiles without the property are returned as-is. The input
// order is preserved.
func Deduplicate(profiles []v1alpha1.ClusterProfile) []v1alpha1.ClusterProfile {
	kept := keep(profiles)
	result := make([]v1alpha1.ClusterProfile, 0, len(profiles))
	for i := range profiles {
		if kept[i] {
			result = append(result, profiles[i])
		}
	}
	return result
}

// Duplicates returns the profiles that Deduplicate removes.
func Duplicates(profiles []v1alpha1.ClusterProfile) []v1alpha1.ClusterProfile {
	kept := keep(profiles)
	var result []v1alpha1.ClusterProfile
	for i := range profiles {
		if !kept[i] {
			result = append(result, profiles[i])
		}
	}
	return result
}

type clusterKey struct {
	namespace string
	id        string
}

// keep reports, for each profile, whether it is the selected representation of
// its member cluster.
func keep(profiles []v1alpha1.ClusterProfile) []bool {
	selected := map[clusterKey]int{}
	for i := range profiles {
		id, ok := ClusterID(&profiles[i])
		if !ok {
			continue
		}
		key := clusterKey{namespace: profiles[i].Namespace, id: id}
		if j, seen := selected[key]; !seen || precedes(&profiles[i], &profiles[j]) {
			selected[key] = i
		}
	}
	result := make([]bool, len(profiles))
	for i := range profiles {
		id, ok := ClusterID(&profiles[i])
		if !ok {
			result[i] = true
			continue
		}
		result[i] = selected[clusterKey{namespace: profiles[i].Namespace, id: id}] == i
	}
	return result
}

// precedes reports whether a is selected over b: the older creationTimestamp
// wins, with ties broken by lexicographic order of name.
func precedes(a, b *v1alpha1.ClusterProfile) bool {
	if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
		return a.CreationTimestamp.Before(&b.CreationTimestamp)
	}
	return a.Name < b.Name
}
