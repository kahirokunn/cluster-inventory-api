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

import (
	"errors"
	"testing"
)

func TestFindProperty(t *testing.T) {
	props := []PropertyEntry{
		{Name: "foo", Value: "bar"},
		{Name: "baz", Value: "qux"},
	}

	t.Run("property exists", func(t *testing.T) {
		val, found := FindProperty(props, "baz")
		if !found {
			t.Fatal("expected property to be found")
		}
		if val != "qux" {
			t.Errorf("value = %q, want qux", val)
		}
	})

	t.Run("property does not exist", func(t *testing.T) {
		val, found := FindProperty(props, "nonexistent")
		if found {
			t.Error("expected property not to be found")
		}
		if val != "" {
			t.Errorf("value = %q, want empty string", val)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		val, found := FindProperty(nil, "foo")
		if found {
			t.Error("expected property not to be found in empty list")
		}
		if val != "" {
			t.Errorf("value = %q, want empty string", val)
		}
	})
}

func TestSetProperty(t *testing.T) {
	t.Run("add new property", func(t *testing.T) {
		props := []PropertyEntry{
			{Name: "existing", Value: "val"},
		}

		result := SetProperty(props, "new-one", "new-val")
		if len(result) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(result))
		}
		val, found := FindProperty(result, "new-one")
		if !found {
			t.Fatal("expected new property to be found")
		}
		if val != "new-val" {
			t.Errorf("value = %q, want new-val", val)
		}
	})

	t.Run("update existing property", func(t *testing.T) {
		props := []PropertyEntry{
			{Name: "key", Value: "old"},
		}

		result := SetProperty(props, "key", "new")
		if len(result) != 1 {
			t.Fatalf("expected 1 entry (updated in place), got %d", len(result))
		}
		if result[0].Value != "new" {
			t.Errorf("value = %q, want new", result[0].Value)
		}
	})
}

func TestRemoveProperty(t *testing.T) {
	t.Run("remove existing property", func(t *testing.T) {
		props := []PropertyEntry{
			{Name: "a", Value: "1"},
			{Name: "b", Value: "2"},
			{Name: "c", Value: "3"},
		}

		result := RemoveProperty(props, "b")
		if len(result) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(result))
		}
		_, found := FindProperty(result, "b")
		if found {
			t.Error("expected property b to be removed")
		}
	})

	t.Run("remove nonexistent property", func(t *testing.T) {
		props := []PropertyEntry{
			{Name: "a", Value: "1"},
		}

		result := RemoveProperty(props, "nonexistent")
		if len(result) != 1 {
			t.Fatalf("expected 1 entry (unchanged), got %d", len(result))
		}
		if result[0].Name != "a" {
			t.Errorf("expected property a to remain, got %q", result[0].Name)
		}
	})
}

func TestResolvePodCIDRs(t *testing.T) {
	t.Run("normal resolution", func(t *testing.T) {
		props := []PropertyEntry{
			{Name: PodCIDRProperty, Value: "10.10.0.0/16"},
		}

		cidrs, err := ResolvePodCIDRs(props, "test-cp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cidrs) != 1 {
			t.Fatalf("expected 1 CIDR, got %d", len(cidrs))
		}
		if cidrs[0] != "10.10.0.0/16" {
			t.Errorf("CIDR = %q, want 10.10.0.0/16", cidrs[0])
		}
	})

	t.Run("property not found", func(t *testing.T) {
		props := []PropertyEntry{
			{Name: "other-property", Value: "val"},
		}

		_, err := ResolvePodCIDRs(props, "test-cp")
		if err == nil {
			t.Fatal("expected error when property is not found")
		}

		var notFoundErr *PropertyNotFoundError
		if !errors.As(err, &notFoundErr) {
			t.Fatalf("expected PropertyNotFoundError, got %T: %v", err, err)
		}
		if notFoundErr.ClusterProfile != "test-cp" {
			t.Errorf("ClusterProfile = %q, want test-cp", notFoundErr.ClusterProfile)
		}
		if notFoundErr.Property != PodCIDRProperty {
			t.Errorf("Property = %q, want %q", notFoundErr.Property, PodCIDRProperty)
		}
	})
}
