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
	"encoding/base64"
	"testing"
)

func TestBuildAccessProviderPayload(t *testing.T) {
	t.Run("multiple providers sorted by name", func(t *testing.T) {
		providers := []AccessProvider{
			{
				Name:            "z-provider",
				ServerURL:       "https://z.example.com:6443",
				CAData:          []byte("ca-z"),
				SecretNamespace: "ns-z",
				SecretName:      "sec-z",
				SecretKey:       "key-z",
			},
			{
				Name:            "a-provider",
				ServerURL:       "https://a.example.com:6443",
				CAData:          []byte("ca-a"),
				SecretNamespace: "ns-a",
				SecretName:      "sec-a",
				SecretKey:       "key-a",
			},
		}

		payload := BuildAccessProviderPayload(providers)

		apRaw, ok := payload["accessProviders"]
		if !ok {
			t.Fatal("expected accessProviders key in payload")
		}
		apList, ok := apRaw.([]any)
		if !ok {
			t.Fatal("expected []any for accessProviders")
		}
		if len(apList) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(apList))
		}

		// Verify sorted order: a-provider first, z-provider second.
		first := apList[0].(map[string]any)
		second := apList[1].(map[string]any)

		if first["name"] != "a-provider" {
			t.Errorf("first entry name = %v, want a-provider", first["name"])
		}
		if second["name"] != "z-provider" {
			t.Errorf("second entry name = %v, want z-provider", second["name"])
		}

		// Verify cluster info on first entry.
		clusterInfo := first["cluster"].(map[string]any)
		if clusterInfo["server"] != "https://a.example.com:6443" {
			t.Errorf("server = %v, want https://a.example.com:6443", clusterInfo["server"])
		}

		// Verify base64 encoding of CA data.
		caEncoded := clusterInfo["certificate-authority-data"].(string)
		caDecoded, err := base64.StdEncoding.DecodeString(caEncoded)
		if err != nil {
			t.Fatalf("base64 decode error: %v", err)
		}
		if string(caDecoded) != "ca-a" {
			t.Errorf("CA data = %q, want %q", string(caDecoded), "ca-a")
		}

		// Verify extensions.
		extensions := clusterInfo["extensions"].([]any)
		if len(extensions) != 1 {
			t.Fatalf("expected 1 extension, got %d", len(extensions))
		}
		ext := extensions[0].(map[string]any)
		if ext["name"] != "client.authentication.k8s.io/exec" {
			t.Errorf("extension name = %v", ext["name"])
		}
		extObj := ext["extension"].(map[string]any)
		if extObj["namespace"] != "ns-a" {
			t.Errorf("extension namespace = %v, want ns-a", extObj["namespace"])
		}
		if extObj["name"] != "sec-a" {
			t.Errorf("extension name = %v, want sec-a", extObj["name"])
		}
		if extObj["key"] != "key-a" {
			t.Errorf("extension key = %v, want key-a", extObj["key"])
		}
	})

	t.Run("single provider", func(t *testing.T) {
		providers := []AccessProvider{
			{
				Name:            "only-one",
				ServerURL:       "https://server:6443",
				CAData:          []byte("ca"),
				SecretNamespace: "ns",
				SecretName:      "sec",
				SecretKey:       "key",
			},
		}

		payload := BuildAccessProviderPayload(providers)
		apList := payload["accessProviders"].([]any)
		if len(apList) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(apList))
		}
		entry := apList[0].(map[string]any)
		if entry["name"] != "only-one" {
			t.Errorf("name = %v, want only-one", entry["name"])
		}
	})

	t.Run("empty list", func(t *testing.T) {
		payload := BuildAccessProviderPayload(nil)
		apRaw := payload["accessProviders"]
		apList, ok := apRaw.([]any)
		if !ok {
			t.Fatal("expected []any for empty providers")
		}
		if len(apList) != 0 {
			t.Errorf("expected empty slice, got %d entries", len(apList))
		}
	})

	t.Run("base64 encoding verification", func(t *testing.T) {
		caData := []byte("hello-world-ca-data")
		providers := []AccessProvider{
			{
				Name:            "test",
				ServerURL:       "https://server:6443",
				CAData:          caData,
				SecretNamespace: "ns",
				SecretName:      "sec",
				SecretKey:       "key",
			},
		}

		payload := BuildAccessProviderPayload(providers)
		entry := payload["accessProviders"].([]any)[0].(map[string]any)
		clusterInfo := entry["cluster"].(map[string]any)
		encoded := clusterInfo["certificate-authority-data"].(string)

		expected := base64.StdEncoding.EncodeToString(caData)
		if encoded != expected {
			t.Errorf("CA base64 = %q, want %q", encoded, expected)
		}
	})
}
