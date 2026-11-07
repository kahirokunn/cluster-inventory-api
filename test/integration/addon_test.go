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

package integration

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	api "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

const (
	addonTestSharedNamespace = "shared"
	addonTestClusterName     = "cluster"
)

var _ = ginkgo.Describe("Addon API schema", func() {
	ginkgo.It("defaults deletion policy and accepts resolved status references", func(ctx context.Context) {
		name := "addon-" + rand.String(5)
		addon, err := clusterProfileClient.ApisV1alpha1().Addons(testNamespace).Create(ctx, &api.Addon{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: api.AddonSpec{
				ClassRef: api.AddonClassReference{
					Name: "class", Namespace: addonTestSharedNamespace,
				},
				ClusterProfileRef: api.AddonClusterProfileReference{Name: addonTestClusterName},
				ParametersRef:     &api.AddonParametersReference{Group: "", Kind: "ConfigMap", Name: "settings"},
			},
		}, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(addon.Spec.DeletionPolicy).To(gomega.Equal(api.AddonDeletionPolicyDelete))

		addon.Status.ClassRef = &api.AddonClassStatusReference{
			Name: "class", Namespace: addonTestSharedNamespace, ControllerName: "example.io/manager",
		}
		addon.Status.ClusterProfileRef = &api.AddonClusterProfileStatusReference{
			Name: addonTestClusterName, UID: types.UID("cluster-uid"),
		}
		_, err = clusterProfileClient.ApisV1alpha1().Addons(testNamespace).UpdateStatus(ctx, addon, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		addon.Status.ClusterProfileRef.UID = ""
		_, err = clusterProfileClient.ApisV1alpha1().Addons(testNamespace).UpdateStatus(ctx, addon, metav1.UpdateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("enforces Class syntax and immutable controllerName", func(ctx context.Context) {
		name := "class-" + rand.String(5)
		classes := clusterProfileClient.ApisV1alpha1().AddonClasses(testNamespace)
		_, err := classes.Create(ctx, &api.AddonClass{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       api.AddonClassSpec{ControllerName: "not-a-domain-path"},
		}, metav1.CreateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())

		class, err := classes.Create(ctx, &api.AddonClass{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: api.AddonClassSpec{ControllerName: "example.io/manager", ParametersRef: &api.AddonClassParametersReference{
				Group: "", Kind: "ConfigMap", Name: "settings", Namespace: addonTestSharedNamespace,
			}},
		}, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		class.Spec.ControllerName = "example.io/other"
		_, err = classes.Update(ctx, class, metav1.UpdateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})

	ginkgo.It("allows Class changes but keeps the target ClusterProfile immutable", func(ctx context.Context) {
		name := "addon-" + rand.String(5)
		addons := clusterProfileClient.ApisV1alpha1().Addons(testNamespace)
		addon, err := addons.Create(ctx, &api.Addon{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: api.AddonSpec{
				ClassRef:          api.AddonClassReference{Name: "first"},
				ClusterProfileRef: api.AddonClusterProfileReference{Name: addonTestClusterName},
			},
		}, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		addon.Spec.ClassRef.Name = "second"
		addon, err = addons.Update(ctx, addon, metav1.UpdateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		addon.Spec.ClusterProfileRef.Name = "another-cluster"
		_, err = addons.Update(ctx, addon, metav1.UpdateOptions{})
		gomega.Expect(err).To(gomega.HaveOccurred())
	})
})
