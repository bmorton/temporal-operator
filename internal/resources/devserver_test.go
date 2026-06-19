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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func devServerFixture() *temporalv1alpha1.TemporalDevServer {
	return &temporalv1alpha1.TemporalDevServer{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "demo"},
		Spec: temporalv1alpha1.TemporalDevServerSpec{
			Version:    "1.31.1",
			Namespaces: []string{"orders", "billing"},
		},
	}
}

func TestBuildDevServerDeployment(t *testing.T) {
	dev := devServerFixture()
	dep := BuildDevServerDeployment(dev, "temporalio/temporal:1.7.2")

	if dep.Name != "dev" || dep.Namespace != "demo" {
		t.Fatalf("unexpected name/namespace: %s/%s", dep.Name, dep.Namespace)
	}
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 1 {
		t.Fatalf("dev server must be single-replica")
	}
	c := dep.Spec.Template.Spec.Containers[0]
	if c.Image != "temporalio/temporal:1.7.2" {
		t.Fatalf("unexpected image: %s", c.Image)
	}
	args := strings.Join(c.Args, " ")
	for _, want := range []string{"server start-dev", "--ip 0.0.0.0", "--namespace orders", "--namespace billing"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
	ports := map[string]int32{}
	for _, p := range c.Ports {
		ports[p.Name] = p.ContainerPort
	}
	if ports["grpc"] != DevServerFrontendPort || ports["ui"] != DevServerUIPort {
		t.Fatalf("unexpected ports: %+v", ports)
	}
}

func TestBuildDevServerDeploymentUIDisabled(t *testing.T) {
	dev := devServerFixture()
	dev.Spec.UI = &temporalv1alpha1.DevServerUISpec{Enabled: false}
	dep := BuildDevServerDeployment(dev, "temporalio/temporal:1.7.2")
	args := strings.Join(dep.Spec.Template.Spec.Containers[0].Args, " ")
	if !strings.Contains(args, "--headless") {
		t.Fatalf("UI-disabled dev server must pass --headless, got %q", args)
	}
}

func TestDevServerImageMapsServerVersion(t *testing.T) {
	dev := devServerFixture() // Version 1.31.1
	img, err := DevServerImage(dev)
	if err != nil {
		t.Fatalf("DevServerImage: %v", err)
	}
	if img != "temporalio/temporal:1.7.2" {
		t.Fatalf("image = %q, want temporalio/temporal:1.7.2", img)
	}
}

func TestDevServerImageDefaultsToLatestSupported(t *testing.T) {
	dev := devServerFixture()
	dev.Spec.Version = ""
	img, err := DevServerImage(dev)
	if err != nil {
		t.Fatalf("DevServerImage: %v", err)
	}
	if !strings.HasPrefix(img, "temporalio/temporal:") {
		t.Fatalf("expected a temporalio/temporal image, got %q", img)
	}
}

func TestDevServerImageEscapeHatch(t *testing.T) {
	dev := devServerFixture()
	dev.Spec.Image = "ghcr.io/me/temporal:dev"
	img, err := DevServerImage(dev)
	if err != nil {
		t.Fatalf("DevServerImage: %v", err)
	}
	if img != "ghcr.io/me/temporal:dev" {
		t.Fatalf("image = %q, want the explicit override", img)
	}
}

func TestDevServerImageUnsupportedVersion(t *testing.T) {
	dev := devServerFixture()
	dev.Spec.Version = "9.9.9"
	if _, err := DevServerImage(dev); err == nil {
		t.Fatal("expected an error for an unsupported version, got nil")
	}
}

func TestDevServerImageUnknownPatchResolvesToLine(t *testing.T) {
	dev := devServerFixture()
	dev.Spec.Version = "1.31.999" // unknown exact patch, but the 1.31 line is known
	img, err := DevServerImage(dev)
	if err != nil {
		t.Fatalf("DevServerImage: %v", err)
	}
	if img != "temporalio/temporal:1.7.2" {
		t.Fatalf("image = %q, want temporalio/temporal:1.7.2 (resolved by minor line)", img)
	}
}

func TestDevServerServerVersion(t *testing.T) {
	dev := devServerFixture() // 1.31.1
	if got := DevServerServerVersion(dev); got != "1.31.1" {
		t.Fatalf("server version = %q, want 1.31.1", got)
	}
	dev.Spec.Version = ""
	if got := DevServerServerVersion(dev); got == "" {
		t.Fatal("expected latest-supported server version, got empty")
	}
	dev = devServerFixture()
	dev.Spec.Image = "ghcr.io/me/temporal:dev"
	if got := DevServerServerVersion(dev); got != "" {
		t.Fatalf("with image override, server version = %q, want empty", got)
	}
}

func TestBuildDevServerService(t *testing.T) {
	dev := devServerFixture()
	svc := BuildDevServerService(dev)
	if svc.Name != DevServerFrontendServiceName("dev") {
		t.Fatalf("unexpected service name: %s", svc.Name)
	}
	ports := map[string]int32{}
	for _, p := range svc.Spec.Ports {
		ports[p.Name] = p.Port
	}
	if ports["grpc"] != DevServerFrontendPort || ports["ui"] != DevServerUIPort {
		t.Fatalf("unexpected service ports: %+v", ports)
	}
}

func TestBuildDevServerEphemeralVolume(t *testing.T) {
	dev := devServerFixture()
	dep := BuildDevServerDeployment(dev, "temporalio/temporal:1.7.2")
	vols := dep.Spec.Template.Spec.Volumes
	if len(vols) != 1 || vols[0].EmptyDir == nil {
		t.Fatalf("ephemeral storage must use an emptyDir volume, got %+v", vols)
	}
}

func TestBuildDevServerPodSecurityContext(t *testing.T) {
	dev := devServerFixture()
	dep := BuildDevServerDeployment(dev, "temporalio/temporal:1.7.2")
	sc := dep.Spec.Template.Spec.SecurityContext
	if sc == nil || sc.FSGroup == nil || *sc.FSGroup != 1000 {
		t.Fatalf("expected pod SecurityContext.FSGroup=1000, got %+v", sc)
	}
}

func TestBuildDevServerPersistentVolume(t *testing.T) {
	dev := devServerFixture()
	dev.Spec.Storage = &temporalv1alpha1.DevServerStorageSpec{Type: "Persistent"}
	dep := BuildDevServerDeployment(dev, "temporalio/temporal:1.7.2")
	vols := dep.Spec.Template.Spec.Volumes
	if len(vols) != 1 || vols[0].PersistentVolumeClaim == nil {
		t.Fatalf("persistent storage must use a PVC volume, got %+v", vols)
	}
	if vols[0].PersistentVolumeClaim.ClaimName != DevServerPVCName("dev") {
		t.Fatalf("unexpected claim name: %s", vols[0].PersistentVolumeClaim.ClaimName)
	}
}
