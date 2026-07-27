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

package access_test

import (
	"bytes"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/cluster-inventory-api/apis/v1alpha2"
	"sigs.k8s.io/cluster-inventory-api/pkg/access"
)

func TestToClientcmdCluster(t *testing.T) {
	rawData := []byte(`{"audience":"example"}`)
	objectData := []byte(`{"tenant":"example"}`)
	cluster := v1alpha2.Cluster{
		Server:                   "https://api.example.com:6443",
		TLSServerName:            "api.example.com",
		InsecureSkipTLSVerify:    true,
		CertificateAuthorityData: []byte("ca-data"),
		ProxyURL:                 "socks5://proxy.example.com:1080",
		DisableCompression:       true,
		Extensions: []v1alpha2.NamedExtension{
			{
				Name: "raw-extension",
				Extension: runtime.RawExtension{
					Raw: rawData,
				},
			},
			{
				Name: "object-extension",
				Extension: runtime.RawExtension{
					Object: &runtime.Unknown{Raw: objectData},
				},
			},
		},
	}

	converted := access.ToClientcmdCluster(cluster)

	if converted.Server != cluster.Server {
		t.Errorf("Server = %q, want %q", converted.Server, cluster.Server)
	}
	if converted.TLSServerName != cluster.TLSServerName {
		t.Errorf("TLSServerName = %q, want %q", converted.TLSServerName, cluster.TLSServerName)
	}
	if converted.InsecureSkipTLSVerify != cluster.InsecureSkipTLSVerify {
		t.Errorf("InsecureSkipTLSVerify = %t, want %t", converted.InsecureSkipTLSVerify, cluster.InsecureSkipTLSVerify)
	}
	if converted.CertificateAuthority != "" {
		t.Errorf("CertificateAuthority = %q, want empty", converted.CertificateAuthority)
	}
	if !bytes.Equal(converted.CertificateAuthorityData, cluster.CertificateAuthorityData) {
		t.Errorf(
			"CertificateAuthorityData = %q, want %q",
			converted.CertificateAuthorityData,
			cluster.CertificateAuthorityData,
		)
	}
	if converted.ProxyURL != cluster.ProxyURL {
		t.Errorf("ProxyURL = %q, want %q", converted.ProxyURL, cluster.ProxyURL)
	}
	if converted.DisableCompression != cluster.DisableCompression {
		t.Errorf("DisableCompression = %t, want %t", converted.DisableCompression, cluster.DisableCompression)
	}
	if len(converted.Extensions) != len(cluster.Extensions) {
		t.Fatalf("Extensions length = %d, want %d", len(converted.Extensions), len(cluster.Extensions))
	}
	for idx := range cluster.Extensions {
		if converted.Extensions[idx].Name != cluster.Extensions[idx].Name {
			t.Errorf(
				"Extensions[%d].Name = %q, want %q",
				idx,
				converted.Extensions[idx].Name,
				cluster.Extensions[idx].Name,
			)
		}
	}

	converted.CertificateAuthorityData[0] = 'X'
	if bytes.Equal(converted.CertificateAuthorityData, cluster.CertificateAuthorityData) {
		t.Error("CertificateAuthorityData shares storage with the input")
	}

	converted.Extensions[0].Extension.Raw[0] = 'X'
	if bytes.Equal(converted.Extensions[0].Extension.Raw, cluster.Extensions[0].Extension.Raw) {
		t.Error("RawExtension.Raw shares storage with the input")
	}

	convertedObject, ok := converted.Extensions[1].Extension.Object.(*runtime.Unknown)
	if !ok {
		t.Fatalf(
			"Extensions[1].Extension.Object has type %T, want *runtime.Unknown",
			converted.Extensions[1].Extension.Object,
		)
	}
	convertedObject.Raw[0] = 'X'
	inputObject := cluster.Extensions[1].Extension.Object.(*runtime.Unknown)
	if bytes.Equal(convertedObject.Raw, inputObject.Raw) {
		t.Error("RawExtension.Object shares storage with the input")
	}
}
