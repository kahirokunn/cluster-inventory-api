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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildConditionMap(t *testing.T) {
	t.Run("all fields present", func(t *testing.T) {
		before := time.Now().UTC()
		m := BuildConditionMap("TestCondition", metav1.ConditionTrue, "TestReason", "test message", 42)
		after := time.Now().UTC()

		if m["type"] != "TestCondition" {
			t.Errorf("type = %v, want TestCondition", m["type"])
		}
		if m["status"] != "True" {
			t.Errorf("status = %v, want True", m["status"])
		}
		if m["reason"] != "TestReason" {
			t.Errorf("reason = %v, want TestReason", m["reason"])
		}
		if m["message"] != "test message" {
			t.Errorf("message = %v, want 'test message'", m["message"])
		}
		if m["observedGeneration"] != int64(42) {
			t.Errorf("observedGeneration = %v, want 42", m["observedGeneration"])
		}

		// Verify lastTransitionTime is RFC3339 and within bounds.
		ltt, ok := m["lastTransitionTime"].(string)
		if !ok {
			t.Fatal("lastTransitionTime is not a string")
		}
		parsed, err := time.Parse(time.RFC3339, ltt)
		if err != nil {
			t.Fatalf("lastTransitionTime parse error: %v", err)
		}
		if parsed.Before(before.Truncate(time.Second)) {
			t.Errorf("lastTransitionTime %v is before test start %v", parsed, before)
		}
		if parsed.After(after.Add(time.Second)) {
			t.Errorf("lastTransitionTime %v is after test end %v", parsed, after)
		}
	})

	t.Run("ConditionTrue status conversion", func(t *testing.T) {
		m := BuildConditionMap("Ready", metav1.ConditionTrue, "R", "msg", 0)
		if m["status"] != "True" {
			t.Errorf("status = %v, want True", m["status"])
		}
	})

	t.Run("ConditionFalse status conversion", func(t *testing.T) {
		m := BuildConditionMap("Ready", metav1.ConditionFalse, "R", "msg", 0)
		if m["status"] != "False" {
			t.Errorf("status = %v, want False", m["status"])
		}
	})

	t.Run("observedGeneration value", func(t *testing.T) {
		m := BuildConditionMap("X", metav1.ConditionTrue, "Y", "Z", 100)
		gen, ok := m["observedGeneration"].(int64)
		if !ok {
			t.Fatal("observedGeneration is not int64")
		}
		if gen != 100 {
			t.Errorf("observedGeneration = %d, want 100", gen)
		}
	})
}
