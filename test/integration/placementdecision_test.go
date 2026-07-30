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
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	cpv1alpha2 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha2"
)

var _ = ginkgo.Describe("PlacementDecisionAPI test", func() {
	var decisionName string

	ginkgo.BeforeEach(func() {
		decisionName = fmt.Sprintf("decision-%s", rand.String(5))
	})

	ginkgo.It("Should create a PlacementDecision", func(ctx context.Context) {
		placementDecision := &cpv1alpha2.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name: decisionName,
				Labels: map[string]string{
					cpv1alpha2.DecisionKeyLabel: "test-decision",
				},
			},
			Decisions: []cpv1alpha2.ClusterDecision{
				{
					ClusterProfileRef: cpv1alpha2.ClusterProfileReference{
						Name: "cluster-1",
					},
					Reason: "best-fit",
				},
				{
					ClusterProfileRef: cpv1alpha2.ClusterProfileReference{
						Name:      "cluster-2",
						Namespace: "other-ns",
					},
				},
			},
			SchedulerName: "test-scheduler",
		}

		_, err := clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Create(
			ctx,
			placementDecision,
			metav1.CreateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should update a PlacementDecision", func(ctx context.Context) {
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
			SchedulerName: "scheduler-v1",
		}

		placementDecision, err := clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Create(
			ctx,
			placementDecision,
			metav1.CreateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		updated := placementDecision.DeepCopy()
		updated.Decisions = append(updated.Decisions, cpv1alpha2.ClusterDecision{
			ClusterProfileRef: cpv1alpha2.ClusterProfileReference{
				Name: "cluster-2",
			},
			Reason: "best-fit",
		})
		updated.SchedulerName = "scheduler-v2"

		_, err = clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Update(
			ctx,
			updated,
			metav1.UpdateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		got, err := clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Get(
			ctx,
			decisionName,
			metav1.GetOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(got.Decisions).To(gomega.HaveLen(2))
		gomega.Expect(got.SchedulerName).To(gomega.Equal("scheduler-v2"))
	})

	ginkgo.It("Should delete a PlacementDecision", func(ctx context.Context) {
		placementDecision := &cpv1alpha2.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name: decisionName,
			},
			Decisions: []cpv1alpha2.ClusterDecision{},
		}

		_, err := clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Create(
			ctx,
			placementDecision,
			metav1.CreateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		err = clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Delete(
			ctx,
			decisionName,
			metav1.DeleteOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Get(
			ctx,
			decisionName,
			metav1.GetOptions{},
		)
		gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
	})

	ginkgo.It("Should reject a PlacementDecision exceeding 100 decisions", func(ctx context.Context) {
		decisions := make([]cpv1alpha2.ClusterDecision, 101)
		for i := range decisions {
			decisions[i] = cpv1alpha2.ClusterDecision{
				ClusterProfileRef: cpv1alpha2.ClusterProfileReference{
					Name: fmt.Sprintf("cluster-%d", i),
				},
			}
		}

		placementDecision := &cpv1alpha2.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{
				Name: decisionName,
			},
			Decisions: decisions,
		}

		_, err := clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Create(
			ctx,
			placementDecision,
			metav1.CreateOptions{},
		)
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(errors.IsInvalid(err)).To(gomega.BeTrue())
	})

	ginkgo.It("Should reject an update that grows a PlacementDecision beyond 100 decisions", func(ctx context.Context) {
		decisions := make([]cpv1alpha2.ClusterDecision, 100)
		for i := range decisions {
			decisions[i] = cpv1alpha2.ClusterDecision{
				ClusterProfileRef: cpv1alpha2.ClusterProfileReference{
					Name: fmt.Sprintf("cluster-%d", i),
				},
			}
		}

		placementDecision, err := clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Create(
			ctx,
			&cpv1alpha2.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{Name: decisionName},
				Decisions:  decisions,
			},
			metav1.CreateOptions{},
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		updated := placementDecision.DeepCopy()
		updated.Decisions = append(updated.Decisions, cpv1alpha2.ClusterDecision{
			ClusterProfileRef: cpv1alpha2.ClusterProfileReference{
				Name: "cluster-last",
			},
		})

		_, err = clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Update(
			ctx,
			updated,
			metav1.UpdateOptions{},
		)
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(errors.IsInvalid(err)).To(gomega.BeTrue())
	})

	ginkgo.It("Should accept a PlacementDecision with a dotted cluster profile name", func(ctx context.Context) {
		placementDecision := &cpv1alpha2.PlacementDecision{
			ObjectMeta: metav1.ObjectMeta{Name: decisionName},
			Decisions: []cpv1alpha2.ClusterDecision{
				{
					ClusterProfileRef: cpv1alpha2.ClusterProfileReference{
						Name:      "cluster.example.com",
						Namespace: "fleet-system",
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
	})

	ginkgo.DescribeTable("Should reject a PlacementDecision with an invalid cluster profile name",
		func(ctx context.Context, invalidName string) {
			placementDecision := &cpv1alpha2.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{Name: decisionName},
				Decisions: []cpv1alpha2.ClusterDecision{
					{
						ClusterProfileRef: cpv1alpha2.ClusterProfileReference{
							Name: invalidName,
						},
					},
				},
			}

			_, err := clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Create(
				ctx,
				placementDecision,
				metav1.CreateOptions{},
			)
			gomega.Expect(errors.IsInvalid(err)).To(gomega.BeTrue())
		},
		ginkgo.Entry("empty", ""),
		ginkgo.Entry("uppercase", "Uppercase-Name"),
		ginkgo.Entry("trailing dash", "trailing-dash-"),
		ginkgo.Entry("254 characters", strings.Repeat("a", 254)),
	)

	ginkgo.DescribeTable("Should reject a PlacementDecision with an invalid cluster profile namespace",
		func(ctx context.Context, invalidNamespace string) {
			placementDecision := &cpv1alpha2.PlacementDecision{
				ObjectMeta: metav1.ObjectMeta{Name: decisionName},
				Decisions: []cpv1alpha2.ClusterDecision{
					{
						ClusterProfileRef: cpv1alpha2.ClusterProfileReference{
							Name:      "cluster-1",
							Namespace: invalidNamespace,
						},
					},
				},
			}

			_, err := clusterProfileClient.ApisV1alpha2().PlacementDecisions(testNamespace).Create(
				ctx,
				placementDecision,
				metav1.CreateOptions{},
			)
			gomega.Expect(errors.IsInvalid(err)).To(gomega.BeTrue())
		},
		ginkgo.Entry("underscore", "Invalid_NS"),
		ginkgo.Entry("dotted", "dotted.ns"),
		ginkgo.Entry("64 characters", strings.Repeat("a", 64)),
	)

})
