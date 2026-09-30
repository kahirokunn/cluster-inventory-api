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

package capiclustertoclusterprofile

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	reconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "capi_cluster_reconciliation_duration_seconds",
			Help:    "Duration of CAPI Cluster reconciliation in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"result"}, // "success", "error", "requeue"
	)

	accessProviderResolutionFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "capi_access_provider_resolution_failures_total",
			Help: "Total number of access provider resolution failures",
		},
		[]string{"reason"}, // "endpoint_not_available", "secret_read_failed"
	)
)

func init() {
	metrics.Registry.MustRegister(
		reconciliationDuration,
		accessProviderResolutionFailures,
	)
}
