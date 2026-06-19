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

package resources

import (
	"testing"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

const testNamespace = "temporal-system"

func testMigration() *temporalv1alpha1.TemporalMigration {
	return &temporalv1alpha1.TemporalMigration{}
}

func TestBuildMigrationProxyService(t *testing.T) {
	m := testMigration()
	m.Name = "orders-migration"
	m.Namespace = testNamespace
	svc := BuildMigrationProxyService(m)
	if svc.Name != "orders-migration-proxy" {
		t.Errorf("service name = %q", svc.Name)
	}
	if svc.Namespace != testNamespace {
		t.Errorf("service namespace = %q", svc.Namespace)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 7233 {
		t.Errorf("service ports = %+v, want one port 7233", svc.Spec.Ports)
	}
}

func TestBuildMigrationProxyDeploymentConfigHash(t *testing.T) {
	m := testMigration()
	m.Name = "orders-migration"
	m.Namespace = testNamespace
	dep := BuildMigrationProxyDeployment(m, "img:latest", "deadbeef")
	got := dep.Spec.Template.Annotations[ConfigHashAnnotation]
	if got != "deadbeef" {
		t.Errorf("config-hash annotation = %q, want deadbeef", got)
	}
	if dep.Spec.Template.Spec.Containers[0].Image != "img:latest" {
		t.Errorf("image = %q", dep.Spec.Template.Spec.Containers[0].Image)
	}
}
