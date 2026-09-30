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

// Package wellknown provides well-known label keys and property names used
// by the CAPI-to-ClusterProfile controller and related components.
package wellknown

// LabelDomainSuffix is the domain suffix used for controller-owned labels.
const LabelDomainSuffix = ".clusterprofiles.multicluster.x-k8s.io"

// Labels used to link a ClusterProfile back to its source CAPI Cluster.
const (
	// LabelSourceClusterName identifies the CAPI Cluster name that
	// produced this ClusterProfile.
	LabelSourceClusterName = "source.clusterprofiles.multicluster.x-k8s.io/cluster-name"

	// LabelSourceClusterNamespace identifies the CAPI Cluster namespace
	// that produced this ClusterProfile.
	LabelSourceClusterNamespace = "source.clusterprofiles.multicluster.x-k8s.io/cluster-namespace"

	// LabelClusterProfileName allows a CAPI Cluster to override the
	// target ClusterProfile name via a label.
	LabelClusterProfileName = "clusterprofile.clusterprofiles.multicluster.x-k8s.io/name"

	// LabelClusterProfileNamespace allows a CAPI Cluster to override
	// the target ClusterProfile namespace via a label.
	LabelClusterProfileNamespace = "clusterprofile.clusterprofiles.multicluster.x-k8s.io/namespace"
)
