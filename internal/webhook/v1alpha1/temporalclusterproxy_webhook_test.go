/*
Copyright 2026 Brian Morton.

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

package v1alpha1

import (
	"context"
	"testing"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func validServerProxy() *temporalv1alpha1.TemporalClusterProxy {
	return &temporalv1alpha1.TemporalClusterProxy{
		Spec: temporalv1alpha1.TemporalClusterProxySpec{
			LocalClusterRef:  temporalv1alpha1.ClusterReference{Name: "local-cluster"},
			LocalClusterName: "local",
			Peer: temporalv1alpha1.ProxyPeer{
				Name: "remote",
			},
			Mux: temporalv1alpha1.ProxyMux{
				Role: temporalv1alpha1.ProxyRoleServer,
				Server: &temporalv1alpha1.ProxyMuxServer{
					ListenPort: 7600,
				},
				TLS: temporalv1alpha1.ProxyMuxTLS{
					Provider:  "cert-manager",
					IssuerRef: &temporalv1alpha1.IssuerReference{Name: "my-issuer"},
				},
			},
		},
	}
}

func TestTemporalClusterProxyValidateCreate(t *testing.T) {
	v := &TemporalClusterProxyCustomValidator{}
	ctx := context.Background()

	cases := []struct {
		name    string
		obj     *temporalv1alpha1.TemporalClusterProxy
		wantErr bool
	}{
		{
			name:    "valid server proxy",
			obj:     validServerProxy(),
			wantErr: false,
		},
		{
			name: "server role without server block",
			obj: func() *temporalv1alpha1.TemporalClusterProxy {
				p := validServerProxy()
				p.Spec.Mux.Server = nil
				return p
			}(),
			wantErr: true,
		},
		{
			name: "client role without client block",
			obj: func() *temporalv1alpha1.TemporalClusterProxy {
				p := validServerProxy()
				p.Spec.Mux.Role = temporalv1alpha1.ProxyRoleClient
				p.Spec.Mux.Server = nil
				return p
			}(),
			wantErr: true,
		},
		{
			name: "cert-manager provider without issuerRef",
			obj: func() *temporalv1alpha1.TemporalClusterProxy {
				p := validServerProxy()
				p.Spec.Mux.TLS.IssuerRef = nil
				return p
			}(),
			wantErr: true,
		},
		{
			name: "secret provider without secretRef",
			obj: func() *temporalv1alpha1.TemporalClusterProxy {
				p := validServerProxy()
				p.Spec.Mux.TLS.Provider = "secret"
				p.Spec.Mux.TLS.IssuerRef = nil
				return p
			}(),
			wantErr: true,
		},
		{
			name: "peer name equals localClusterName",
			obj: func() *temporalv1alpha1.TemporalClusterProxy {
				p := validServerProxy()
				p.Spec.Peer.Name = "local"
				return p
			}(),
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.ValidateCreate(ctx, tc.obj)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v got err=%v", tc.wantErr, err)
			}
		})
	}
}

func TestTemporalClusterProxyValidateUpdate(t *testing.T) {
	v := &TemporalClusterProxyCustomValidator{}
	ctx := context.Background()

	t.Run("immutable mux.role", func(t *testing.T) {
		old := validServerProxy()
		newP := validServerProxy()
		newP.Spec.Mux.Role = temporalv1alpha1.ProxyRoleClient
		newP.Spec.Mux.Server = nil
		newP.Spec.Mux.Client = &temporalv1alpha1.ProxyMuxClient{ServerAddress: "remote:7600"}
		_, err := v.ValidateUpdate(ctx, old, newP)
		if err == nil {
			t.Fatal("expected error for immutable mux.role, got nil")
		}
	})

	t.Run("immutable localClusterRef", func(t *testing.T) {
		old := validServerProxy()
		newP := validServerProxy()
		newP.Spec.LocalClusterRef.Name = "different-cluster"
		_, err := v.ValidateUpdate(ctx, old, newP)
		if err == nil {
			t.Fatal("expected error for immutable localClusterRef, got nil")
		}
	})

	t.Run("valid update", func(t *testing.T) {
		old := validServerProxy()
		newP := validServerProxy()
		newP.Spec.Peer.Name = "new-remote"
		_, err := v.ValidateUpdate(ctx, old, newP)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestTemporalClusterProxyDefaulter(t *testing.T) {
	d := &TemporalClusterProxyCustomDefaulter{}
	ctx := context.Background()

	t.Run("defaults are filled", func(t *testing.T) {
		p := &temporalv1alpha1.TemporalClusterProxy{
			Spec: temporalv1alpha1.TemporalClusterProxySpec{
				Peer: temporalv1alpha1.ProxyPeer{},
				Mux:  temporalv1alpha1.ProxyMux{},
			},
		}
		if err := d.Default(ctx, p); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Spec.Mux.TLS.Provider != "cert-manager" {
			t.Errorf("expected provider=cert-manager, got %q", p.Spec.Mux.TLS.Provider)
		}
		if p.Spec.Peer.EnableConnection == nil || !*p.Spec.Peer.EnableConnection {
			t.Error("expected peer.enableConnection=true")
		}
		if p.Spec.Image != defaultProxyImage {
			t.Errorf("expected image=%q, got %q", defaultProxyImage, p.Spec.Image)
		}
	})

	t.Run("existing values are not overwritten", func(t *testing.T) {
		enable := false
		p := &temporalv1alpha1.TemporalClusterProxy{
			Spec: temporalv1alpha1.TemporalClusterProxySpec{
				Image: "my-custom-image:v1",
				Peer: temporalv1alpha1.ProxyPeer{
					EnableConnection: &enable,
				},
				Mux: temporalv1alpha1.ProxyMux{
					TLS: temporalv1alpha1.ProxyMuxTLS{Provider: "secret"},
				},
			},
		}
		if err := d.Default(ctx, p); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Spec.Mux.TLS.Provider != "secret" {
			t.Errorf("expected provider=secret, got %q", p.Spec.Mux.TLS.Provider)
		}
		if p.Spec.Peer.EnableConnection == nil || *p.Spec.Peer.EnableConnection {
			t.Error("expected peer.enableConnection=false (not overwritten)")
		}
		if p.Spec.Image != "my-custom-image:v1" {
			t.Errorf("expected image=my-custom-image:v1, got %q", p.Spec.Image)
		}
	})
}
