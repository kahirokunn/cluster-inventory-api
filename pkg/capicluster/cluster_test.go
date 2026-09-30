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

package capicluster

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestExtractPodCIDRs(t *testing.T) {
	tests := []struct {
		name string
		obj  map[string]any
		want []string
	}{
		{
			name: "multiple CIDRs",
			obj: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{"10.10.0.0/16", "10.11.0.0/16"},
						},
					},
				},
			},
			want: []string{"10.10.0.0/16", "10.11.0.0/16"},
		},
		{
			name: "single CIDR",
			obj: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{"10.10.0.0/16"},
						},
					},
				},
			},
			want: []string{"10.10.0.0/16"},
		},
		{
			name: "empty cidrBlocks",
			obj: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "spec missing clusterNetwork",
			obj: map[string]any{
				"spec": map[string]any{},
			},
			want: nil,
		},
		{
			name: "cidrBlocks field absent",
			obj: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{},
					},
				},
			},
			want: nil,
		},
		{
			name: "non-string element in cidrBlocks",
			obj: map[string]any{
				"spec": map[string]any{
					"clusterNetwork": map[string]any{
						"pods": map[string]any{
							"cidrBlocks": []any{42},
						},
					},
				},
			},
			want: nil, // NestedStringSlice returns error for non-string
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &unstructured.Unstructured{Object: tt.obj}
			got := ExtractPodCIDRs(cluster)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ExtractPodCIDRs() = %v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractPodCIDRs() len = %d, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("ExtractPodCIDRs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSafeClusterFields(t *testing.T) {
	t.Run("all fields present", func(t *testing.T) {
		u := &unstructured.Unstructured{}
		u.SetName("my-cluster")
		u.SetNamespace("ns-1")
		u.SetUID("abc-123")

		ts := metav1.NewTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
		u.SetDeletionTimestamp(&ts)

		fields := SafeClusterFields(u)
		m := toMap(fields)

		if m["name"] != "my-cluster" {
			t.Errorf("name = %v, want my-cluster", m["name"])
		}
		if m["namespace"] != "ns-1" {
			t.Errorf("namespace = %v, want ns-1", m["namespace"])
		}
		if m["uid"] != "abc-123" {
			t.Errorf("uid = %v, want abc-123", m["uid"])
		}
		if _, ok := m["deletionTimestamp"]; !ok {
			t.Error("expected deletionTimestamp to be present")
		}
	})

	t.Run("partial fields - no UID no deletionTimestamp", func(t *testing.T) {
		u := &unstructured.Unstructured{}
		u.SetName("c1")
		u.SetNamespace("ns")

		fields := SafeClusterFields(u)
		m := toMap(fields)

		if m["name"] != "c1" {
			t.Errorf("name = %v, want c1", m["name"])
		}
		if _, ok := m["uid"]; ok {
			t.Error("uid should not be present when empty")
		}
		if _, ok := m["deletionTimestamp"]; ok {
			t.Error("deletionTimestamp should not be present when nil")
		}
	})

	t.Run("deletionTimestamp nil", func(t *testing.T) {
		u := &unstructured.Unstructured{}
		u.SetName("c2")
		u.SetNamespace("ns")

		fields := SafeClusterFields(u)
		m := toMap(fields)

		if _, ok := m["deletionTimestamp"]; ok {
			t.Error("deletionTimestamp should not be present")
		}
	})
}

func TestIsDomainLabel(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		domainSuffix string
		want         bool
	}{
		{
			name:         "domain match (suffix)",
			key:          "environment.clusterprofiles.multicluster.x-k8s.io",
			domainSuffix: ".clusterprofiles.multicluster.x-k8s.io",
			want:         true,
		},
		{
			name:         "domain mismatch",
			key:          "environment.example.com",
			domainSuffix: ".clusterprofiles.multicluster.x-k8s.io",
			want:         false,
		},
		{
			name:         "key with slash - prefix matches",
			key:          "role.clusterprofiles.multicluster.x-k8s.io/workload",
			domainSuffix: ".clusterprofiles.multicluster.x-k8s.io",
			want:         true,
		},
		{
			name:         "key with slash - prefix does not match",
			key:          "role.example.com/workload",
			domainSuffix: ".clusterprofiles.multicluster.x-k8s.io",
			want:         false,
		},
		{
			name:         "empty key",
			key:          "",
			domainSuffix: ".clusterprofiles.multicluster.x-k8s.io",
			want:         false,
		},
		{
			name:         "no prefix (bare key)",
			key:          "simple-label",
			domainSuffix: ".clusterprofiles.multicluster.x-k8s.io",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsDomainLabel(tt.key, tt.domainSuffix)
			if got != tt.want {
				t.Errorf("IsDomainLabel(%q, %q) = %v, want %v", tt.key, tt.domainSuffix, got, tt.want)
			}
		})
	}
}

func TestExtractDomainLabels(t *testing.T) {
	suffix := ".clusterprofiles.multicluster.x-k8s.io"

	tests := []struct {
		name   string
		labels map[string]string
		want   map[string]string
	}{
		{
			name: "multiple matching labels",
			labels: map[string]string{
				"env" + suffix:            "prod",
				"region.aws" + suffix:     "us-east-1",
				"unrelated.example.com":   "ignored",
				"role" + suffix + "/main": "true",
			},
			want: map[string]string{
				"env" + suffix:            "prod",
				"region.aws" + suffix:     "us-east-1",
				"role" + suffix + "/main": "true",
			},
		},
		{
			name: "no matching labels",
			labels: map[string]string{
				"cluster.clusterset.k8s.io": "fleet-a",
				"other-label":               "val",
			},
			want: map[string]string{},
		},
		{
			name: "key with slash",
			labels: map[string]string{
				"source" + suffix + "/cluster-name": "my-cluster",
			},
			want: map[string]string{
				"source" + suffix + "/cluster-name": "my-cluster",
			},
		},
		{
			name:   "empty label map",
			labels: map[string]string{},
			want:   map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &unstructured.Unstructured{}
			cluster.SetLabels(tt.labels)

			got := ExtractDomainLabels(cluster, suffix)

			if len(got) != len(tt.want) {
				t.Fatalf("ExtractDomainLabels() len = %d, want %d; got %v", len(got), len(tt.want), got)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("ExtractDomainLabels()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// toMap converts a []any of key-value pairs into a map for easier testing.
func toMap(fields []any) map[string]any {
	m := make(map[string]any)
	for i := 0; i < len(fields)-1; i += 2 {
		k, ok := fields[i].(string)
		if !ok {
			continue
		}
		m[k] = fields[i+1]
	}
	return m
}
