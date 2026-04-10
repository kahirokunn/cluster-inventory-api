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

package wellknown

import "fmt"

// Well-known property names for ClusterProfile.status.properties.
const (
	// PodCIDRProperty is the canonical property name for pod CIDR.
	PodCIDRProperty = "pod-cidr.clusterprofiles.multicluster.x-k8s.io"

	// ClusterIDProperty is the property name for the cluster ID.
	ClusterIDProperty = "cluster-id.clusterprofiles.multicluster.x-k8s.io"

	// SubnetIDProperty is the canonical property name for subnet ID.
	SubnetIDProperty = "subnet-id.clusterprofiles.multicluster.x-k8s.io"

	// AWSRegionProperty is the AWS region property in ClusterProfile.
	AWSRegionProperty = "default-region.aws.clusterprofiles.multicluster.x-k8s.io"
)

// PropertyNotFoundError is returned when a required property is not found.
type PropertyNotFoundError struct {
	ClusterProfile string
	Property       string
}

func (e *PropertyNotFoundError) Error() string {
	return fmt.Sprintf("ClusterProfile %s does not have %s property", e.ClusterProfile, e.Property)
}

// PropertyEntry represents a single status.properties entry.
type PropertyEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// FindProperty looks up a property by name in a list of properties.
// Returns the value and true if found, or empty string and false if not.
func FindProperty(properties []PropertyEntry, name string) (string, bool) {
	for _, p := range properties {
		if p.Name == name {
			return p.Value, true
		}
	}
	return "", false
}

// SetProperty sets or updates a property in a list of properties.
// Returns the updated list.
func SetProperty(properties []PropertyEntry, name, value string) []PropertyEntry {
	for i := range properties {
		if properties[i].Name == name {
			properties[i].Value = value
			return properties
		}
	}
	return append(properties, PropertyEntry{Name: name, Value: value})
}

// RemoveProperty removes a property by name from a list.
// Returns the updated list.
func RemoveProperty(properties []PropertyEntry, name string) []PropertyEntry {
	for i := range properties {
		if properties[i].Name == name {
			return append(properties[:i], properties[i+1:]...)
		}
	}
	return properties
}

// ResolvePodCIDRs resolves Pod CIDRs from ClusterProfile properties.
// It looks for the canonical PodCIDRProperty and returns it as a single-element slice.
func ResolvePodCIDRs(properties []PropertyEntry, clusterProfileName string) ([]string, error) {
	value, found := FindProperty(properties, PodCIDRProperty)
	if !found {
		return nil, &PropertyNotFoundError{
			ClusterProfile: clusterProfileName,
			Property:       PodCIDRProperty,
		}
	}
	return []string{value}, nil
}
