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
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	v1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

func TestBuildStatusPatchPayload(t *testing.T) {
	t.Run("normal payload structure", func(t *testing.T) {
		key := types.NamespacedName{Name: "my-cp", Namespace: "my-ns"}
		desiredStatus := map[string]any{
			"properties": []any{
				map[string]any{"name": "foo", "value": "bar"},
			},
		}

		obj := BuildStatusPatchPayload(key, desiredStatus)

		if obj.GetName() != "my-cp" {
			t.Errorf("name = %q, want my-cp", obj.GetName())
		}
		if obj.GetNamespace() != "my-ns" {
			t.Errorf("namespace = %q, want my-ns", obj.GetNamespace())
		}

		// Verify GVK is set to ClusterProfile.
		gvk := obj.GetObjectKind().GroupVersionKind()
		expected := v1alpha1.ClusterProfileSchemeGroupVersionKind
		if gvk != expected {
			t.Errorf("GVK = %v, want %v", gvk, expected)
		}

		// Verify status is set.
		statusRaw, found, err := unstructured.NestedMap(obj.Object, "status")
		if err != nil {
			t.Fatalf("NestedMap error: %v", err)
		}
		if !found {
			t.Fatal("expected status field")
		}
		propsRaw, ok := statusRaw["properties"]
		if !ok {
			t.Fatal("expected properties in status")
		}
		props, ok := propsRaw.([]any)
		if !ok {
			t.Fatal("expected []any for properties")
		}
		if len(props) != 1 {
			t.Fatalf("expected 1 property, got %d", len(props))
		}
	})

	t.Run("empty status map", func(t *testing.T) {
		key := types.NamespacedName{Name: "empty-cp", Namespace: "ns"}
		obj := BuildStatusPatchPayload(key, map[string]any{})

		statusRaw, found, err := unstructured.NestedMap(obj.Object, "status")
		if err != nil {
			t.Fatalf("NestedMap error: %v", err)
		}
		if !found {
			t.Fatal("expected status field even for empty map")
		}
		if len(statusRaw) != 0 {
			t.Errorf("expected empty status map, got %v", statusRaw)
		}
	})

	t.Run("GVK is set correctly", func(t *testing.T) {
		key := types.NamespacedName{Name: "gvk-test", Namespace: "ns"}
		obj := BuildStatusPatchPayload(key, map[string]any{"foo": "bar"})

		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Group != "multicluster.x-k8s.io" {
			t.Errorf("group = %q, want multicluster.x-k8s.io", gvk.Group)
		}
		if gvk.Version != "v1alpha1" {
			t.Errorf("version = %q, want v1alpha1", gvk.Version)
		}
		if gvk.Kind != "ClusterProfile" {
			t.Errorf("kind = %q, want ClusterProfile", gvk.Kind)
		}
	})

	t.Run("managedFields is nil", func(t *testing.T) {
		key := types.NamespacedName{Name: "mf-test", Namespace: "ns"}
		obj := BuildStatusPatchPayload(key, map[string]any{})

		if obj.GetManagedFields() != nil {
			t.Error("expected managedFields to be nil")
		}
	})
}

func TestStubClient_ApplyStatus(t *testing.T) {
	stub := &StubClient{}
	key := types.NamespacedName{Name: "x", Namespace: "y"}
	err := stub.ApplyStatus(context.Background(), key, nil, StatusPatchOptions{})
	if err != ErrNotImplemented {
		t.Errorf("StubClient.ApplyStatus() = %v, want ErrNotImplemented", err)
	}
}
