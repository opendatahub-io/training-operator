/*
Copyright 2021.

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

package main

import (
	"crypto/tls"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func makeAPIServer(spec map[string]interface{}) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(apiServerGVK())
	if spec != nil {
		obj.Object["spec"] = spec
	}
	return obj
}

func apiServerGVK() (gvk struct {
	Group, Version, Kind string
}) {
	return struct{ Group, Version, Kind string }{
		Group:   "config.openshift.io",
		Version: "v1",
		Kind:    "APIServer",
	}
}

func TestParseTLSProfile(t *testing.T) {
	tests := []struct {
		name           string
		apiServer      *unstructured.Unstructured
		wantMinVersion uint16
		wantCiphers    []uint16
	}{
		{
			name:           "nil profile returns Intermediate defaults",
			apiServer:      makeAPIServer(nil),
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    intermediateCiphers,
		},
		{
			name:           "missing tlsSecurityProfile returns Intermediate defaults",
			apiServer:      makeAPIServer(map[string]interface{}{}),
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    intermediateCiphers,
		},
		{
			name: "Intermediate profile returns Intermediate defaults",
			apiServer: makeAPIServer(map[string]interface{}{
				"tlsSecurityProfile": map[string]interface{}{
					"type": "Intermediate",
				},
			}),
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    intermediateCiphers,
		},
		{
			name: "empty profile type returns Intermediate defaults",
			apiServer: makeAPIServer(map[string]interface{}{
				"tlsSecurityProfile": map[string]interface{}{
					"type": "",
				},
			}),
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    intermediateCiphers,
		},
		{
			name: "Modern returns TLS 1.3 with nil ciphers",
			apiServer: makeAPIServer(map[string]interface{}{
				"tlsSecurityProfile": map[string]interface{}{
					"type": "Modern",
				},
			}),
			wantMinVersion: tls.VersionTLS13,
			wantCiphers:    nil,
		},
		{
			name: "Old returns TLS 1.2 with nil ciphers",
			apiServer: makeAPIServer(map[string]interface{}{
				"tlsSecurityProfile": map[string]interface{}{
					"type": "Old",
				},
			}),
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    nil,
		},
		{
			name: "Custom with valid ciphers",
			apiServer: makeAPIServer(map[string]interface{}{
				"tlsSecurityProfile": map[string]interface{}{
					"type": "Custom",
					"custom": map[string]interface{}{
						"minTLSVersion": "VersionTLS13",
						"ciphers": []interface{}{
							"ECDHE-ECDSA-AES128-GCM-SHA256",
							"ECDHE-RSA-AES256-GCM-SHA384",
						},
					},
				},
			}),
			wantMinVersion: tls.VersionTLS13,
			wantCiphers: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			},
		},
		{
			name: "Custom with DHE cipher logs and skips unsupported",
			apiServer: makeAPIServer(map[string]interface{}{
				"tlsSecurityProfile": map[string]interface{}{
					"type": "Custom",
					"custom": map[string]interface{}{
						"minTLSVersion": "VersionTLS12",
						"ciphers": []interface{}{
							"ECDHE-ECDSA-AES128-GCM-SHA256",
							"DHE-RSA-AES128-GCM-SHA256",
						},
					},
				},
			}),
			wantMinVersion: tls.VersionTLS12,
			wantCiphers: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			},
		},
		{
			name: "Custom with nil custom block returns Intermediate defaults",
			apiServer: makeAPIServer(map[string]interface{}{
				"tlsSecurityProfile": map[string]interface{}{
					"type": "Custom",
				},
			}),
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    intermediateCiphers,
		},
		{
			name: "Custom with unknown minTLSVersion falls back to TLS 1.2",
			apiServer: makeAPIServer(map[string]interface{}{
				"tlsSecurityProfile": map[string]interface{}{
					"type": "Custom",
					"custom": map[string]interface{}{
						"minTLSVersion": "VersionTLS11",
						"ciphers": []interface{}{
							"ECDHE-RSA-AES128-GCM-SHA256",
						},
					},
				},
			}),
			wantMinVersion: tls.VersionTLS12,
			wantCiphers: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			},
		},
		{
			name: "unknown profile type returns Intermediate defaults",
			apiServer: makeAPIServer(map[string]interface{}{
				"tlsSecurityProfile": map[string]interface{}{
					"type": "Futuristic",
				},
			}),
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    intermediateCiphers,
		},
		{
			name: "Custom with all unsupported ciphers returns empty slice",
			apiServer: makeAPIServer(map[string]interface{}{
				"tlsSecurityProfile": map[string]interface{}{
					"type": "Custom",
					"custom": map[string]interface{}{
						"minTLSVersion": "VersionTLS12",
						"ciphers": []interface{}{
							"DHE-RSA-AES128-GCM-SHA256",
							"DHE-RSA-AES256-GCM-SHA384",
						},
					},
				},
			}),
			wantMinVersion: tls.VersionTLS12,
			wantCiphers:    []uint16{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMinVersion, gotCiphers := parseTLSProfile(tt.apiServer)

			if gotMinVersion != tt.wantMinVersion {
				t.Errorf("minVersion = 0x%04x, want 0x%04x", gotMinVersion, tt.wantMinVersion)
			}

			if tt.wantCiphers == nil {
				if gotCiphers != nil {
					t.Errorf("ciphers = %v, want nil", gotCiphers)
				}
				return
			}

			if gotCiphers == nil {
				t.Fatal("expected non-nil empty slice, got nil (fail-closed guard needs non-nil)")
			}
			if len(gotCiphers) != len(tt.wantCiphers) {
				t.Fatalf("ciphers length = %d, want %d", len(gotCiphers), len(tt.wantCiphers))
			}
			for i, c := range gotCiphers {
				if c != tt.wantCiphers[i] {
					t.Errorf("ciphers[%d] = 0x%04x, want 0x%04x", i, c, tt.wantCiphers[i])
				}
			}
		})
	}
}

func TestTLSVersionMap(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want uint16
	}{
		{name: "TLS 1.2", key: "VersionTLS12", want: tls.VersionTLS12},
		{name: "TLS 1.3", key: "VersionTLS13", want: tls.VersionTLS13},
		{name: "unknown version returns zero", key: "VersionTLS11", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tlsVersionMap[tt.key]
			if got != tt.want {
				t.Errorf("tlsVersionMap[%q] = 0x%04x, want 0x%04x", tt.key, got, tt.want)
			}
		})
	}
}
