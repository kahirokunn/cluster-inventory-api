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

package inventory

import (
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

func TestInventory(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Inventory Package Suite")
}

var base = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func profile(namespace, name, id string, created time.Time) v1alpha1.ClusterProfile {
	p := v1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              name,
			CreationTimestamp: metav1.NewTime(created),
		},
	}
	if id != "" {
		p.Status.Properties = []v1alpha1.Property{
			{Name: v1alpha1.PropertyClusterID, Value: id},
		}
	}
	return p
}

func names(profiles []v1alpha1.ClusterProfile) []string {
	result := []string{}
	for _, p := range profiles {
		result = append(result, p.Namespace+"/"+p.Name)
	}
	return result
}

var _ = ginkgo.Describe("ClusterID", func() {
	ginkgo.It("returns the value of the id.k8s.io property", func() {
		p := profile("ns", "cluster-1", "uid-1", base)
		id, ok := ClusterID(&p)
		gomega.Expect(ok).To(gomega.BeTrue())
		gomega.Expect(id).To(gomega.Equal("uid-1"))
	})

	ginkgo.It("reports a missing property", func() {
		p := profile("ns", "cluster-1", "", base)
		_, ok := ClusterID(&p)
		gomega.Expect(ok).To(gomega.BeFalse())
	})
})

var _ = ginkgo.Describe("Deduplicate", func() {
	ginkgo.It("keeps profiles of distinct clusters", func() {
		profiles := []v1alpha1.ClusterProfile{
			profile("ns", "cluster-1", "uid-1", base),
			profile("ns", "cluster-2", "uid-2", base),
		}
		gomega.Expect(names(Deduplicate(profiles))).To(gomega.Equal(
			[]string{"ns/cluster-1", "ns/cluster-2"}))
	})

	ginkgo.It("keeps only the oldest profile of a duplicated cluster", func() {
		profiles := []v1alpha1.ClusterProfile{
			profile("ns", "newer", "uid-1", base.Add(time.Hour)),
			profile("ns", "older", "uid-1", base),
		}
		gomega.Expect(names(Deduplicate(profiles))).To(gomega.Equal(
			[]string{"ns/older"}))
	})

	ginkgo.It("breaks creationTimestamp ties by name", func() {
		profiles := []v1alpha1.ClusterProfile{
			profile("ns", "b-cluster", "uid-1", base),
			profile("ns", "a-cluster", "uid-1", base),
		}
		gomega.Expect(names(Deduplicate(profiles))).To(gomega.Equal(
			[]string{"ns/a-cluster"}))
	})

	ginkgo.It("keeps profiles without the id property", func() {
		profiles := []v1alpha1.ClusterProfile{
			profile("ns", "cluster-1", "", base),
			profile("ns", "cluster-2", "", base),
		}
		gomega.Expect(names(Deduplicate(profiles))).To(gomega.Equal(
			[]string{"ns/cluster-1", "ns/cluster-2"}))
	})

	ginkgo.It("does not deduplicate across namespaces", func() {
		profiles := []v1alpha1.ClusterProfile{
			profile("argocd", "prod", "uid-1", base),
			profile("kueue", "prod", "uid-1", base.Add(time.Hour)),
		}
		gomega.Expect(names(Deduplicate(profiles))).To(gomega.Equal(
			[]string{"argocd/prod", "kueue/prod"}))
	})

	ginkgo.It("preserves the input order", func() {
		profiles := []v1alpha1.ClusterProfile{
			profile("ns", "cluster-2", "uid-2", base),
			profile("ns", "newer", "uid-1", base.Add(time.Hour)),
			profile("ns", "older", "uid-1", base),
		}
		gomega.Expect(names(Deduplicate(profiles))).To(gomega.Equal(
			[]string{"ns/cluster-2", "ns/older"}))
	})
})

var _ = ginkgo.Describe("Duplicates", func() {
	ginkgo.It("returns the removed profiles", func() {
		profiles := []v1alpha1.ClusterProfile{
			profile("ns", "newer", "uid-1", base.Add(time.Hour)),
			profile("ns", "older", "uid-1", base),
		}
		gomega.Expect(names(Duplicates(profiles))).To(gomega.Equal(
			[]string{"ns/newer"}))
	})

	ginkgo.It("returns nothing when there are no duplicates", func() {
		profiles := []v1alpha1.ClusterProfile{
			profile("ns", "cluster-1", "uid-1", base),
			profile("ns", "cluster-2", "", base),
		}
		gomega.Expect(Duplicates(profiles)).To(gomega.BeEmpty())
	})
})
