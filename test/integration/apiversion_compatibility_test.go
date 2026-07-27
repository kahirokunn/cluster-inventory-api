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

package integration

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	clientcmdv1 "k8s.io/client-go/tools/clientcmd/api/v1"

	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpv1alpha2 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha2"
)

var _ = ginkgo.Describe("API version compatibility", func() {
	ginkgo.It("serves the same ClusterProfile through v1alpha1 and v1alpha2", func(ctx context.Context) {
		clusterName := fmt.Sprintf("cluster-compat-%s", rand.String(5))
		clusterManagerName := fmt.Sprintf("cluster-manager-%s", rand.String(5))

		clusterProfile := &cpv1alpha1.ClusterProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					cpv1alpha1.LabelClusterManagerKey: clusterManagerName,
				},
			},
			Spec: cpv1alpha1.ClusterProfileSpec{
				DisplayName: clusterName,
				ClusterManager: cpv1alpha1.ClusterManager{
					Name: clusterManagerName,
				},
			},
		}

		clusterProfile, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(testNamespace).Create(
			ctx,
			clusterProfile,
			metav1.CreateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		updated := clusterProfile.DeepCopy()
		updated.Status.AccessProviders = []cpv1alpha1.AccessProvider{
			{
				Name: "access",
				Cluster: clientcmdv1.Cluster{
					Server: "https://access.example.com",
				},
			},
		}
		updated.Status.CredentialProviders = []cpv1alpha1.CredentialProvider{
			{
				Name: "credential",
				Cluster: clientcmdv1.Cluster{
					Server: "https://credential.example.com",
				},
			},
		}

		_, err = clusterProfileClient.ApisV1alpha1().ClusterProfiles(testNamespace).UpdateStatus(
			ctx,
			updated,
			metav1.UpdateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gotV1Alpha2, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(testNamespace).Get(
			ctx,
			clusterName,
			metav1.GetOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(gotV1Alpha2.Status.AccessProviders).To(gomega.HaveLen(1))
		gomega.Expect(gotV1Alpha2.Status.AccessProviders[0].Name).To(gomega.Equal("access"))
	})

	ginkgo.It("serves the same v1alpha2 ClusterProfile through v1alpha1", func(ctx context.Context) {
		clusterName := fmt.Sprintf("cluster-v1alpha2-%s", rand.String(5))
		clusterManagerName := fmt.Sprintf("cluster-manager-%s", rand.String(5))

		clusterProfile := &cpv1alpha2.ClusterProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
				Labels: map[string]string{
					cpv1alpha2.LabelClusterManagerKey: clusterManagerName,
				},
			},
			Spec: cpv1alpha2.ClusterProfileSpec{
				DisplayName: clusterName,
				ClusterManager: cpv1alpha2.ClusterManager{
					Name: clusterManagerName,
				},
			},
		}

		_, err := clusterProfileClient.ApisV1alpha2().ClusterProfiles(testNamespace).Create(
			ctx,
			clusterProfile,
			metav1.CreateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gotV1Alpha1, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(testNamespace).Get(
			ctx,
			clusterName,
			metav1.GetOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(gotV1Alpha1.Name).To(gomega.Equal(clusterName))
		gomega.Expect(gotV1Alpha1.Status.CredentialProviders).To(gomega.BeEmpty())
	})

	ginkgo.It("serves the same PlacementDecision through v1alpha1 and v1alpha2", func(ctx context.Context) {
		decisionName := fmt.Sprintf("decision-compat-%s", rand.String(5))

		placementDecision := &cpv1alpha2.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name: decisionName,
			},
			Decisions: []cpv1alpha2.ClusterDecision{
				{
					ClusterProfileRef: cpv1alpha2.ClusterProfileReference{
						Name: "cluster-1",
					},
				},
			},
		}

		_, err := clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Create(
			ctx,
			placementDecision,
			metav1.CreateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		gotV1Alpha1, err := clusterProfileClient.ApisV1alpha1().PlacementDecisions(testNamespace).Get(
			ctx,
			decisionName,
			metav1.GetOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(gotV1Alpha1.Decisions).To(gomega.HaveLen(1))
		gomega.Expect(gotV1Alpha1.Decisions[0].ClusterProfileRef.Name).To(gomega.Equal("cluster-1"))
	})

	ginkgo.It("keeps v1alpha1 ClusterProfile validation backward compatible", func(ctx context.Context) {
		clusterName := fmt.Sprintf("cluster-legacy-%s", rand.String(5))

		clusterProfile, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(testNamespace).Create(
			ctx,
			&cpv1alpha1.ClusterProfile{
				ObjectMeta: metav1.ObjectMeta{Name: clusterName},
				Spec: cpv1alpha1.ClusterProfileSpec{
					ClusterManager: cpv1alpha1.ClusterManager{Name: "-legacy-manager"},
				},
			},
			metav1.CreateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		updated := clusterProfile.DeepCopy()
		updated.Status.AccessProviders = []cpv1alpha1.AccessProvider{
			{
				Name: "",
				Cluster: clientcmdv1.Cluster{
					Server:               "not-a-url",
					CertificateAuthority: "/etc/legacy-ca.crt",
				},
			},
		}

		_, err = clusterProfileClient.ApisV1alpha1().ClusterProfiles(testNamespace).UpdateStatus(
			ctx,
			updated,
			metav1.UpdateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("keeps v1alpha1 PlacementDecision reference validation backward compatible", func(ctx context.Context) {
		decisionName := fmt.Sprintf("decision-legacy-%s", rand.String(5))

		_, err := clusterProfileClient.ApisV1alpha1().PlacementDecisions(testNamespace).Create(
			ctx,
			&cpv1alpha1.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{Name: decisionName},
				Decisions: []cpv1alpha1.ClusterDecision{
					{
						ClusterProfileRef: cpv1alpha1.ClusterProfileReference{
							Name:      "Legacy_Name",
							Namespace: "legacy.namespace",
						},
					},
				},
			},
			metav1.CreateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
})
