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

func conn(peers ...temporalv1alpha1.ClusterConnectionPeer) *temporalv1alpha1.TemporalClusterConnection {
	return &temporalv1alpha1.TemporalClusterConnection{
		Spec: temporalv1alpha1.TemporalClusterConnectionSpec{Peers: peers},
	}
}

func localPeer(name, ref string) temporalv1alpha1.ClusterConnectionPeer {
	return temporalv1alpha1.ClusterConnectionPeer{Name: name, ClusterRef: &temporalv1alpha1.ClusterReference{Name: ref}}
}

func TestClusterConnectionValidateCreate(t *testing.T) {
	v := &TemporalClusterConnectionCustomValidator{}
	cases := []struct {
		name    string
		obj     *temporalv1alpha1.TemporalClusterConnection
		wantErr bool
	}{
		{"valid", conn(localPeer("a", "cluster-a"), localPeer("b", "cluster-b")), false},
		{"too-few", conn(localPeer("a", "cluster-a")), true},
		{"dup-names", conn(localPeer("a", "cluster-a"), localPeer("a", "cluster-b")), true},
		{"no-source", conn(localPeer("a", "cluster-a"), temporalv1alpha1.ClusterConnectionPeer{Name: "b"}), true},
		{"both-source", conn(localPeer("a", "cluster-a"), temporalv1alpha1.ClusterConnectionPeer{
			Name: "b", ClusterRef: &temporalv1alpha1.ClusterReference{Name: "x"}, FrontendAddress: "y:7233",
		}), true},
		{"tls-without-external", conn(localPeer("a", "cluster-a"), temporalv1alpha1.ClusterConnectionPeer{
			Name: "b", ClusterRef: &temporalv1alpha1.ClusterReference{Name: "x"},
			TLSSecretRef: &temporalv1alpha1.SecretReference{Name: "s"},
		}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.ValidateCreate(context.Background(), tc.obj)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v got err=%v", tc.wantErr, err)
			}
		})
	}
}
