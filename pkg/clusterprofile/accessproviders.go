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
	"encoding/base64"
	"sort"
)

// AccessProvider describes one entry for status.accessProviders.
type AccessProvider struct {
	Name            string
	ServerURL       string
	CAData          []byte
	SecretNamespace string
	SecretName      string
	SecretKey       string
}

// BuildAccessProviderPayload builds the SSA status payload for accessProviders.
// The returned map is suitable for use with StatusApplier.ApplyStatus().
//
// The payload matches the ClusterProfile CRD structure:
//
//	accessProviders[].name
//	accessProviders[].cluster.server
//	accessProviders[].cluster.certificate-authority-data
//	accessProviders[].cluster.extensions[].name
//	accessProviders[].cluster.extensions[].extension
//
// Providers are sorted by Name for deterministic SSA output.
// Nil or empty providers produce a map with an empty slice.
func BuildAccessProviderPayload(providers []AccessProvider) map[string]any {
	if len(providers) == 0 {
		return map[string]any{"accessProviders": []any{}}
	}

	// Sort by name for deterministic output.
	sorted := make([]AccessProvider, len(providers))
	copy(sorted, providers)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	items := make([]any, len(sorted))
	for i, p := range sorted {
		items[i] = map[string]any{
			"name": p.Name,
			"cluster": map[string]any{
				"server":                     p.ServerURL,
				"certificate-authority-data": base64.StdEncoding.EncodeToString(p.CAData),
				"extensions": []any{
					map[string]any{
						"name": "client.authentication.k8s.io/exec",
						"extension": map[string]any{
							"namespace": p.SecretNamespace,
							"name":      p.SecretName,
							"key":       p.SecretKey,
						},
					},
				},
			},
		}
	}
	return map[string]any{"accessProviders": items}
}
