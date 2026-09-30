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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

func TestBuildApplyList(t *testing.T) {
	t.Run("sorted output", func(t *testing.T) {
		owned := OwnedProperties{
			"z-property": "z-val",
			"a-property": "a-val",
			"m-property": "m-val",
		}

		list := BuildApplyList(owned)

		if len(list) != 3 {
			t.Fatalf("expected 3 items, got %d", len(list))
		}
		if list[0].Name != "a-property" {
			t.Errorf("first item name = %q, want a-property", list[0].Name)
		}
		if list[1].Name != "m-property" {
			t.Errorf("second item name = %q, want m-property", list[1].Name)
		}
		if list[2].Name != "z-property" {
			t.Errorf("third item name = %q, want z-property", list[2].Name)
		}
		// Verify values
		if list[0].Value != "a-val" {
			t.Errorf("first item value = %q, want a-val", list[0].Value)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		list := BuildApplyList(OwnedProperties{})
		if list != nil {
			t.Errorf("expected nil for empty map, got %v", list)
		}
	})

	t.Run("single entry", func(t *testing.T) {
		owned := OwnedProperties{"only": "one"}
		list := BuildApplyList(owned)
		if len(list) != 1 {
			t.Fatalf("expected 1 item, got %d", len(list))
		}
		if list[0].Name != "only" || list[0].Value != "one" {
			t.Errorf("item = {%q, %q}, want {only, one}", list[0].Name, list[0].Value)
		}
	})
}

func TestBuildApplyPayload(t *testing.T) {
	t.Run("normal payload structure", func(t *testing.T) {
		owned := OwnedProperties{
			"b-prop": "b-val",
			"a-prop": "a-val",
		}

		payload := BuildApplyPayload(owned)
		propsRaw, ok := payload["properties"]
		if !ok {
			t.Fatal("expected properties key in payload")
		}
		items, ok := propsRaw.([]any)
		if !ok {
			t.Fatal("expected []any for properties")
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}

		// Verify sorted order.
		first := items[0].(map[string]any)
		if first["name"] != "a-prop" {
			t.Errorf("first property name = %v, want a-prop", first["name"])
		}
		if first["value"] != "a-val" {
			t.Errorf("first property value = %v, want a-val", first["value"])
		}
	})

	t.Run("empty properties", func(t *testing.T) {
		payload := BuildApplyPayload(OwnedProperties{})
		propsRaw := payload["properties"]
		items, ok := propsRaw.([]any)
		if !ok {
			t.Fatal("expected []any for empty properties")
		}
		if len(items) != 0 {
			t.Errorf("expected empty slice, got %d items", len(items))
		}
	})
}

func TestGetStatusProperty(t *testing.T) {
	t.Run("property exists", func(t *testing.T) {
		cp := &v1alpha1.ClusterProfile{
			Status: v1alpha1.ClusterProfileStatus{
				Properties: []v1alpha1.Property{
					{Name: "foo", Value: "bar"},
					{Name: "baz", Value: "qux"},
				},
			},
		}

		val, found := GetStatusProperty(cp, "baz")
		if !found {
			t.Fatal("expected property to be found")
		}
		if val != "qux" {
			t.Errorf("value = %q, want qux", val)
		}
	})

	t.Run("property does not exist", func(t *testing.T) {
		cp := &v1alpha1.ClusterProfile{
			Status: v1alpha1.ClusterProfileStatus{
				Properties: []v1alpha1.Property{
					{Name: "foo", Value: "bar"},
				},
			},
		}

		val, found := GetStatusProperty(cp, "nonexistent")
		if found {
			t.Error("expected property not to be found")
		}
		if val != "" {
			t.Errorf("value = %q, want empty string", val)
		}
	})

	t.Run("nil status properties", func(t *testing.T) {
		cp := &v1alpha1.ClusterProfile{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}

		val, found := GetStatusProperty(cp, "anything")
		if found {
			t.Error("expected property not to be found for nil status")
		}
		if val != "" {
			t.Errorf("value = %q, want empty string", val)
		}
	})
}
