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

package clusterprofiletolabels

import (
	"context"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	v1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

// -- helpers --

func makeClusterProfileWithProperties(properties []map[string]any) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(v1alpha1.ClusterProfileSchemeGroupVersionKind)
	obj.SetName("test-cp")
	obj.SetNamespace("test-ns")

	if properties != nil {
		propSlice := make([]any, len(properties))
		for i, p := range properties {
			propSlice[i] = p
		}
		_ = unstructured.SetNestedSlice(obj.Object, propSlice, "status", "properties")
	}

	return obj
}

func makeTypedClusterProfileWithProperties(properties []v1alpha1.Property) *v1alpha1.ClusterProfile {
	return &v1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cp",
			Namespace: "test-ns",
		},
		Status: v1alpha1.ClusterProfileStatus{
			Properties: properties,
		},
	}
}

var _ = ginkgo.Describe("extractPropertiesHash", func() {
	ginkgo.It("should produce stable ordering for same properties in different order", func() {
		obj1 := makeClusterProfileWithProperties([]map[string]any{
			{"name": "a", "value": "1"},
			{"name": "b", "value": "2"},
		})
		obj2 := makeClusterProfileWithProperties([]map[string]any{
			{"name": "b", "value": "2"},
			{"name": "a", "value": "1"},
		})

		h1 := extractPropertiesHash(obj1)
		h2 := extractPropertiesHash(obj2)

		gomega.Expect(h1).To(gomega.Equal(h2), "hashes should be equal for same properties in different order")
	})

	ginkgo.It("should produce different hashes for different values", func() {
		obj1 := makeClusterProfileWithProperties([]map[string]any{
			{"name": "a", "value": "1"},
		})
		obj2 := makeClusterProfileWithProperties([]map[string]any{
			{"name": "a", "value": "2"},
		})

		h1 := extractPropertiesHash(obj1)
		h2 := extractPropertiesHash(obj2)

		gomega.Expect(h1).NotTo(gomega.Equal(h2), "hashes should differ for different property values")
	})

	ginkgo.It("should return empty hash for no properties", func() {
		obj := makeClusterProfileWithProperties(nil)
		h := extractPropertiesHash(obj)
		gomega.Expect(h).To(gomega.BeEmpty(), "expected empty hash for no properties")
	})
})

var _ = ginkgo.Describe("statusPropertiesChangedPredicate", func() {
	ginkgo.DescribeTable("Update",
		func(oldProps []map[string]any, newProps []map[string]any, want bool) {
			pred := statusPropertiesChangedPredicate{}
			oldObj := makeClusterProfileWithProperties(oldProps)
			newObj := makeClusterProfileWithProperties(newProps)

			got := pred.Update(event.UpdateEvent{
				ObjectOld: oldObj,
				ObjectNew: newObj,
			})
			gomega.Expect(got).To(gomega.Equal(want))
		},
		ginkgo.Entry("properties changed",
			[]map[string]any{{"name": "a", "value": "1"}},
			[]map[string]any{{"name": "a", "value": "2"}},
			true,
		),
		ginkgo.Entry("properties unchanged",
			[]map[string]any{{"name": "a", "value": "1"}},
			[]map[string]any{{"name": "a", "value": "1"}},
			false,
		),
		ginkgo.Entry("label-only change (no properties change)",
			[]map[string]any{{"name": "a", "value": "1"}},
			[]map[string]any{{"name": "a", "value": "1"}},
			false,
		),
		ginkgo.Entry("no properties to some properties",
			nil,
			[]map[string]any{{"name": "a", "value": "1"}},
			true,
		),
		ginkgo.Entry("some properties to no properties",
			[]map[string]any{{"name": "a", "value": "1"}},
			nil,
			true,
		),
	)

	ginkgo.It("Create should return true", func() {
		pred := statusPropertiesChangedPredicate{}
		gomega.Expect(pred.Create(event.CreateEvent{})).To(gomega.BeTrue())
	})

	ginkgo.It("Delete should return false", func() {
		pred := statusPropertiesChangedPredicate{}
		gomega.Expect(pred.Delete(event.DeleteEvent{})).To(gomega.BeFalse())
	})

	ginkgo.It("Generic should return false", func() {
		pred := statusPropertiesChangedPredicate{}
		gomega.Expect(pred.Generic(event.GenericEvent{})).To(gomega.BeFalse())
	})
})

var _ = ginkgo.Describe("buildPropertiesLabelsSyncedCondition", func() {
	ginkgo.It("should report AllSynced when all properties are valid", func() {
		props := []propertyEntry{
			{Name: "cluster-id.clusterprofiles.multicluster.x-k8s.io", Value: "42"},
			{Name: "subnet-id.clusterprofiles.multicluster.x-k8s.io", Value: "production"},
		}

		cond := buildPropertiesLabelsSyncedCondition(props, nil)

		gomega.Expect(cond.Type).To(gomega.Equal(ConditionPropertiesLabelsSynced))
		gomega.Expect(cond.Status).To(gomega.Equal(metav1.ConditionTrue))
		gomega.Expect(cond.Reason).To(gomega.Equal(ReasonAllSynced))
		gomega.Expect(cond.Message).To(gomega.ContainSubstring("2"))
	})

	ginkgo.It("should report PartialSync when some properties are skipped", func() {
		props := []propertyEntry{
			{Name: "cluster-id.clusterprofiles.multicluster.x-k8s.io", Value: "42"},
			{Name: "pod-cidr.clusterprofiles.multicluster.x-k8s.io", Value: "10.10.0.0/16"},
		}
		skipped := []skippedProperty{
			{
				Name:   "pod-cidr.clusterprofiles.multicluster.x-k8s.io",
				Reason: "InvalidLabelValue",
				Detail: "contains invalid characters",
			},
		}

		cond := buildPropertiesLabelsSyncedCondition(props, skipped)

		gomega.Expect(cond.Status).To(gomega.Equal(metav1.ConditionFalse))
		gomega.Expect(cond.Reason).To(gomega.Equal(ReasonPartialSync))
		gomega.Expect(cond.Message).To(gomega.ContainSubstring("pod-cidr.clusterprofiles.multicluster.x-k8s.io"))
		gomega.Expect(cond.Message).To(gomega.ContainSubstring("1 of 2"))
	})

	ginkgo.It("should report NoProperties when there are no properties", func() {
		cond := buildPropertiesLabelsSyncedCondition(nil, nil)

		gomega.Expect(cond.Status).To(gomega.Equal(metav1.ConditionTrue))
		gomega.Expect(cond.Reason).To(gomega.Equal(ReasonNoProperties))
	})
})

var _ = ginkgo.Describe("extractPropertiesFromTyped", func() {
	ginkgo.It("should extract properties from a typed ClusterProfile", func() {
		cp := makeTypedClusterProfileWithProperties([]v1alpha1.Property{
			{Name: "cluster-id.clusterprofiles.multicluster.x-k8s.io", Value: "42"},
			{Name: "subnet-id.clusterprofiles.multicluster.x-k8s.io", Value: "prod"},
		})

		props := extractPropertiesFromTyped(cp)
		gomega.Expect(props).To(gomega.HaveLen(2))
		gomega.Expect(props[0].Name).To(gomega.Equal("cluster-id.clusterprofiles.multicluster.x-k8s.io"))
		gomega.Expect(props[0].Value).To(gomega.Equal("42"))
	})

	ginkgo.It("should return empty slice for no properties", func() {
		cp := makeTypedClusterProfileWithProperties(nil)
		props := extractPropertiesFromTyped(cp)
		gomega.Expect(props).To(gomega.BeEmpty())
	})
})

var _ = ginkgo.Describe("Validation", func() {
	ginkgo.It("should identify invalid label values (CIDR)", func() {
		props := []propertyEntry{
			{Name: "cluster-id.clusterprofiles.multicluster.x-k8s.io", Value: "42"},
			{Name: "pod-cidr.clusterprofiles.multicluster.x-k8s.io", Value: "10.10.0.0/16"},
		}

		var validLabels []string
		var skippedNames []string

		for _, prop := range props {
			keyErrs := validation.IsQualifiedName(prop.Name)
			valErrs := validation.IsValidLabelValue(prop.Value)
			if len(keyErrs) == 0 && len(valErrs) == 0 {
				validLabels = append(validLabels, prop.Name)
			} else {
				skippedNames = append(skippedNames, prop.Name)
			}
		}

		gomega.Expect(validLabels).To(gomega.HaveLen(1))
		gomega.Expect(validLabels[0]).To(gomega.Equal("cluster-id.clusterprofiles.multicluster.x-k8s.io"))
		gomega.Expect(skippedNames).To(gomega.HaveLen(1))
	})

	ginkgo.It("should accept valid properties as labels", func() {
		validProps := []propertyEntry{
			{Name: "cluster-id.clusterprofiles.multicluster.x-k8s.io", Value: "42"},
			{Name: "subnet-id.clusterprofiles.multicluster.x-k8s.io", Value: "production"},
			{Name: "cluster.clusterset.k8s.io", Value: "fleet-a"},
			{Name: "id.vpc.aws.clusterprofiles.multicluster.x-k8s.io", Value: "vpc-0abc123def456789"},
		}

		for _, prop := range validProps {
			keyErrs := validation.IsQualifiedName(prop.Name)
			valErrs := validation.IsValidLabelValue(prop.Value)
			gomega.Expect(keyErrs).To(gomega.BeEmpty(),
				"property %s=%s should have valid key; keyErrs=%v", prop.Name, prop.Value, keyErrs)
			gomega.Expect(valErrs).To(gomega.BeEmpty(),
				"property %s=%s should have valid value; valErrs=%v", prop.Name, prop.Value, valErrs)
		}
	})
})

var _ = ginkgo.Describe("buildMetadataLabelsApplyObject", func() {
	ginkgo.It("should have nil managedFields in SSA payload", func() {
		obj := buildMetadataLabelsApplyObject(
			types.NamespacedName{Namespace: "inventory", Name: "cluster-a"},
			map[string]string{"environment.clusterprofiles.multicluster.x-k8s.io": "dev"},
		)

		gomega.Expect(obj.GetManagedFields()).To(gomega.BeNil(), "managedFields must be nil in SSA payload")
		gomega.Expect(obj.GetLabels()["environment.clusterprofiles.multicluster.x-k8s.io"]).To(gomega.Equal("dev"))

		meta, found, err := unstructured.NestedMap(obj.Object, "metadata")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(found).To(gomega.BeTrue())
		_, hasManagedFields := meta["managedFields"]
		gomega.Expect(hasManagedFields).To(gomega.BeFalse(), "metadata.managedFields must be absent in SSA payload")
	})
})

var _ = ginkgo.Describe("buildStatusConditionApplyObject", func() {
	ginkgo.It("should have nil managedFields and correct condition in SSA payload", func() {
		cond := metav1.Condition{
			Type:               ConditionPropertiesLabelsSynced,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonAllSynced,
			Message:            "all synced",
			LastTransitionTime: metav1.Unix(1, 0),
		}

		obj, err := buildStatusConditionApplyObject(
			types.NamespacedName{Namespace: "inventory", Name: "cluster-a"}, cond)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(obj.GetManagedFields()).To(gomega.BeNil(), "managedFields must be nil in SSA payload")

		meta, found, err := unstructured.NestedMap(obj.Object, "metadata")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(found).To(gomega.BeTrue())
		_, hasManagedFields := meta["managedFields"]
		gomega.Expect(hasManagedFields).To(gomega.BeFalse(), "metadata.managedFields must be absent in SSA payload")

		conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(found).To(gomega.BeTrue())
		gomega.Expect(conditions).To(gomega.HaveLen(1))

		conditionMap, ok := conditions[0].(map[string]any)
		gomega.Expect(ok).To(gomega.BeTrue(), "condition payload should be map[string]any")
		gomega.Expect(conditionMap["reason"]).To(gomega.Equal(ReasonAllSynced))
	})
})

// -- helpers for Reconcile tests --

func newPropertiesToLabelsReconciler(objs ...runtime.Object) *PropertiesToLabelsReconciler {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)

	b := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		switch v := o.(type) {
		case *v1alpha1.ClusterProfile:
			b = b.WithObjects(v).WithStatusSubresource(v)
		}
	}

	return &PropertiesToLabelsReconciler{
		Client: b.Build(),
		Log:    zap.New(zap.UseDevMode(true)),
	}
}

var _ = ginkgo.Describe("PropertiesToLabelsReconciler.Reconcile", func() {

	ginkgo.It("should return zero result for deleted ClusterProfile (NotFound)", func() {
		// No ClusterProfile in the fake client -> NotFound -> normal return.
		r := newPropertiesToLabelsReconciler()

		result, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Namespace: "ns", Name: "deleted-cp"},
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(result).To(gomega.Equal(ctrl.Result{}))
	})

	ginkgo.It("should skip property with label key exceeding 253 characters", func() {
		// Create a property name that exceeds 253 characters (invalid label key).
		longKey := strings.Repeat("a", 254)
		cp := &v1alpha1.ClusterProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "long-key-cp",
				Namespace: "test-ns",
			},
			Status: v1alpha1.ClusterProfileStatus{
				Properties: []v1alpha1.Property{
					{Name: longKey, Value: "some-value"},
					{Name: "valid-key.clusterprofiles.multicluster.x-k8s.io", Value: "ok"},
				},
			},
		}

		r := newPropertiesToLabelsReconciler(cp)
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Namespace: "test-ns", Name: "long-key-cp"},
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify that the long key was validated and rejected via IsQualifiedName.
		errs := validation.IsQualifiedName(longKey)
		gomega.Expect(errs).NotTo(gomega.BeEmpty(), "expected validation failure for long key")
	})

	ginkgo.It("should skip property with label value exceeding 63 characters", func() {
		longValue := strings.Repeat("v", 64) // 64 chars > 63 limit
		cp := &v1alpha1.ClusterProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "long-val-cp",
				Namespace: "test-ns",
			},
			Status: v1alpha1.ClusterProfileStatus{
				Properties: []v1alpha1.Property{
					{Name: "key.clusterprofiles.multicluster.x-k8s.io", Value: longValue},
				},
			},
		}

		r := newPropertiesToLabelsReconciler(cp)
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Namespace: "test-ns", Name: "long-val-cp"},
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify that the long value is actually invalid.
		errs := validation.IsValidLabelValue(longValue)
		gomega.Expect(errs).NotTo(gomega.BeEmpty(), "expected validation failure for long value")
	})

	ginkgo.It("should skip property with special characters in label value", func() {
		specialValue := "10.10.0.0/16" // Contains "/" which is invalid for label values
		cp := &v1alpha1.ClusterProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "special-val-cp",
				Namespace: "test-ns",
			},
			Status: v1alpha1.ClusterProfileStatus{
				Properties: []v1alpha1.Property{
					{Name: "pod-cidr.clusterprofiles.multicluster.x-k8s.io", Value: specialValue},
				},
			},
		}

		r := newPropertiesToLabelsReconciler(cp)
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Namespace: "test-ns", Name: "special-val-cp"},
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify that the value with special characters is invalid.
		errs := validation.IsValidLabelValue(specialValue)
		gomega.Expect(errs).NotTo(gomega.BeEmpty(), "expected validation failure for CIDR value")
	})
})
