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
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func builderCluster() *temporalv1alpha1.TemporalCluster {
	return &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tc", Namespace: "ns"},
		Spec:       temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1"},
	}
}

func TestEnabledServices(t *testing.T) {
	c := builderCluster()
	svcs := EnabledServices(c)
	if len(svcs) != 4 {
		t.Fatalf("expected 4 core services, got %d", len(svcs))
	}

	c.Spec.Services.InternalFrontend = &temporalv1alpha1.InternalFrontendSpec{Enabled: true}
	if len(EnabledServices(c)) != 5 {
		t.Errorf("expected internal-frontend to be included when enabled")
	}
}

func TestSelectorLabelsStableAcrossVersion(t *testing.T) {
	c := builderCluster()
	sel := SelectorLabels(c, "frontend")
	if _, ok := sel[LabelVersion]; ok {
		t.Errorf("selector labels must not include version")
	}
	full := StandardLabels(c, "frontend")
	if full[LabelVersion] != "1.31.1" {
		t.Errorf("standard labels must include version")
	}
	if full[LabelManagedBy] != managedByValue {
		t.Errorf("standard labels must include managed-by")
	}
}

func TestBuildDeployment(t *testing.T) {
	c := builderCluster()
	svc := EnabledServices(c)[0] // frontend
	dep := BuildDeployment(c, svc, "abc123", "", nil)

	if dep.Name != "tc-frontend" {
		t.Errorf("unexpected name %q", dep.Name)
	}
	ctr := dep.Spec.Template.Spec.Containers[0]
	if ctr.Image != "temporalio/server:1.31.1" {
		t.Errorf("unexpected image %q", ctr.Image)
	}
	if !slices.Contains(ctr.Command, "/bin/sh") {
		t.Errorf("expected shell wrapper command, got %v", ctr.Command)
	}
	if len(ctr.Args) != 1 || !strings.Contains(ctr.Args[0], "--service frontend") {
		t.Errorf("expected --service frontend in args, got %v", ctr.Args)
	}
	if !strings.Contains(ctr.Args[0], "POD_IP") {
		t.Errorf("expected POD_IP substitution in entrypoint, got %v", ctr.Args)
	}
	if dep.Spec.Template.Annotations[ConfigHashAnnotation] != "abc123" {
		t.Errorf("expected config-hash annotation")
	}
	if ctr.StartupProbe == nil || ctr.StartupProbe.GRPC == nil {
		t.Errorf("expected gRPC startup probe")
	}
	if ctr.StartupProbe.FailureThreshold != 30 {
		t.Errorf("expected startup failureThreshold 30")
	}
	// Default topology spread should be applied.
	if len(dep.Spec.Template.Spec.TopologySpreadConstraints) != 1 {
		t.Errorf("expected default topology spread constraint")
	}
}

func TestBuildDeploymentWorkerHasNoProbes(t *testing.T) {
	c := builderCluster()
	var worker ServiceInfo
	for _, svc := range EnabledServices(c) {
		if svc.Name == ServiceWorker {
			worker = svc
		}
	}
	if worker.Name != ServiceWorker {
		t.Fatalf("worker service not found in EnabledServices")
	}

	ctr := BuildDeployment(c, worker, "abc123", "", nil).Spec.Template.Spec.Containers[0]
	// The Temporal worker does not serve a client-facing gRPC endpoint, so it
	// must not get gRPC health probes (matching the upstream Helm chart).
	// Otherwise the startup probe fails forever and the cluster never goes Ready.
	if ctr.StartupProbe != nil || ctr.ReadinessProbe != nil || ctr.LivenessProbe != nil {
		t.Errorf("worker must not have probes, got startup=%v readiness=%v liveness=%v",
			ctr.StartupProbe, ctr.ReadinessProbe, ctr.LivenessProbe)
	}
}

func TestBuildDeploymentMTLSUsesTCPProbes(t *testing.T) {
	c := builderCluster()
	svc := EnabledServices(c)[0] // frontend
	mtls := &MTLSMounts{Enabled: true, InternodeSecret: "tc-internode", FrontendSecret: "tc-frontend-tls"}
	ctr := BuildDeployment(c, svc, "abc123", "", mtls).Spec.Template.Spec.Containers[0]

	if ctr.StartupProbe == nil || ctr.ReadinessProbe == nil || ctr.LivenessProbe == nil {
		t.Fatalf("expected probes on frontend under mTLS")
	}
	// Kubernetes' native gRPC prober connects without a client certificate and
	// cannot complete the mutual-TLS handshake on a requireClientAuth port, so
	// mTLS clusters must use TCP probes instead of gRPC. Otherwise the startup
	// probe fails forever and the cluster never goes Ready.
	if ctr.StartupProbe.GRPC != nil || ctr.ReadinessProbe.GRPC != nil || ctr.LivenessProbe.GRPC != nil {
		t.Errorf("probes must not be gRPC under mTLS")
	}
	if ctr.StartupProbe.TCPSocket == nil || ctr.ReadinessProbe.TCPSocket == nil || ctr.LivenessProbe.TCPSocket == nil {
		t.Errorf("probes must be TCPSocket under mTLS")
	}
	// Probe tuning is preserved across the gRPC/TCP switch.
	if ctr.StartupProbe.FailureThreshold != 30 {
		t.Errorf("expected startup failureThreshold 30, got %d", ctr.StartupProbe.FailureThreshold)
	}
	if ctr.ReadinessProbe.TimeoutSeconds != 3 {
		t.Errorf("expected readiness timeoutSeconds 3, got %d", ctr.ReadinessProbe.TimeoutSeconds)
	}
}

func TestApplyPodTemplateLabelsAnnotationsAndSpec(t *testing.T) {
	base := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      map[string]string{"app.kubernetes.io/component": "frontend"},
			Annotations: map[string]string{"existing": "keep"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "temporal", Image: "temporalio/server:1.31.1"}},
			Volumes:    []corev1.Volume{{Name: "config"}},
		},
	}
	selector := map[string]string{"app.kubernetes.io/component": "frontend"}
	override := &temporalv1alpha1.PodTemplateOverride{
		Labels:      map[string]string{"azure.workload.identity/use": "true"},
		Annotations: map[string]string{"added": "yes"},
		Spec: &runtime.RawExtension{Raw: []byte(`{
			"serviceAccountName": "temporal-azure",
			"containers": [
				{"name": "temporal", "volumeMounts": [{"name": "azure-token", "mountPath": "/azure"}]},
				{"name": "sidecar", "image": "mcr.microsoft.com/azure-cli:latest"}
			],
			"volumes": [{"name": "azure-token", "emptyDir": {}}]
		}`)},
	}

	got, err := applyPodTemplate(base, override, selector)
	if err != nil {
		t.Fatalf("applyPodTemplate returned error: %v", err)
	}

	if got.Labels["azure.workload.identity/use"] != "true" {
		t.Errorf("override label missing: %v", got.Labels)
	}
	if got.Labels["app.kubernetes.io/component"] != "frontend" {
		t.Errorf("selector label must be preserved: %v", got.Labels)
	}
	if got.Annotations["existing"] != "keep" || got.Annotations["added"] != "yes" {
		t.Errorf("annotations not merged: %v", got.Annotations)
	}
	if got.Spec.ServiceAccountName != "temporal-azure" {
		t.Errorf("serviceAccountName not set: %q", got.Spec.ServiceAccountName)
	}
	if len(got.Spec.Containers) != 2 {
		t.Fatalf("expected sidecar appended, got %d containers", len(got.Spec.Containers))
	}
	var temporal *corev1.Container
	for i := range got.Spec.Containers {
		if got.Spec.Containers[i].Name == "temporal" {
			temporal = &got.Spec.Containers[i]
		}
	}
	if temporal == nil || len(temporal.VolumeMounts) != 1 || temporal.VolumeMounts[0].Name != "azure-token" {
		t.Errorf("temporal container volumeMount not merged: %+v", temporal)
	}
	if len(got.Spec.Volumes) != 2 {
		t.Errorf("expected azure-token volume appended, got %d", len(got.Spec.Volumes))
	}
}

func TestApplyPodTemplateNilIsNoop(t *testing.T) {
	base := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "b"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "temporal"}}},
	}
	got, err := applyPodTemplate(base, nil, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Spec.Containers) != 1 || got.Labels["a"] != "b" {
		t.Errorf("nil override must be a no-op, got %+v", got)
	}
}

func TestApplyPodTemplateDoesNotMutateInputMaps(t *testing.T) {
	base := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      map[string]string{"app.kubernetes.io/component": "frontend"},
			Annotations: map[string]string{"existing": "keep"},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "temporal"}}},
	}
	override := &temporalv1alpha1.PodTemplateOverride{
		Labels:      map[string]string{"azure.workload.identity/use": "true"},
		Annotations: map[string]string{"added": "yes"},
	}
	selector := map[string]string{"app.kubernetes.io/component": "frontend", "selector": "required"}

	got, err := applyPodTemplate(base, override, selector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Labels["azure.workload.identity/use"] != "true" || got.Labels["selector"] != "required" {
		t.Fatalf("expected returned template to include merged labels, got %v", got.Labels)
	}
	if got.Annotations["added"] != "yes" {
		t.Fatalf("expected returned template to include merged annotations, got %v", got.Annotations)
	}
	if _, ok := base.Labels["azure.workload.identity/use"]; ok {
		t.Errorf("base labels were mutated: %v", base.Labels)
	}
	if _, ok := base.Labels["selector"]; ok {
		t.Errorf("base labels were mutated by selector re-assertion: %v", base.Labels)
	}
	if _, ok := base.Annotations["added"]; ok {
		t.Errorf("base annotations were mutated: %v", base.Annotations)
	}
}

func TestApplyPodTemplateOverrideCannotDropSelectorLabel(t *testing.T) {
	base := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/component": "frontend"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "temporal"}}},
	}
	selector := map[string]string{"app.kubernetes.io/component": "frontend"}
	override := &temporalv1alpha1.PodTemplateOverride{
		Labels: map[string]string{"app.kubernetes.io/component": "evil"},
	}
	got, err := applyPodTemplate(base, override, selector)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Labels["app.kubernetes.io/component"] != "frontend" {
		t.Errorf("selector label must win over override, got %q", got.Labels["app.kubernetes.io/component"])
	}
}

func TestApplyPodTemplateInvalidSpecErrors(t *testing.T) {
	base := corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "temporal"}}}}
	override := &temporalv1alpha1.PodTemplateOverride{
		Spec: &runtime.RawExtension{Raw: []byte("{ not json")},
	}
	if _, err := applyPodTemplate(base, override, nil); err == nil {
		t.Errorf("expected error for malformed podTemplate spec patch")
	}
}

func TestBuildServicesAndPDB(t *testing.T) {
	c := builderCluster()
	frontend := EnabledServices(c)[0]

	headless := BuildHeadlessService(c, frontend)
	if headless.Spec.ClusterIP != "None" {
		t.Errorf("headless service must have ClusterIP None")
	}

	fe := BuildFrontendService(c, frontend)
	if fe.Name != "tc-frontend" || len(fe.Spec.Ports) != 2 {
		t.Errorf("unexpected frontend service: %s ports=%d", fe.Name, len(fe.Spec.Ports))
	}

	pdb := BuildPodDisruptionBudget(c, frontend)
	if pdb.Spec.MaxUnavailable == nil || pdb.Spec.MaxUnavailable.IntValue() != 1 {
		t.Errorf("expected maxUnavailable 1")
	}
}

func TestConfigBuildersAndHash(t *testing.T) {
	c := builderCluster()
	secret := BuildConfigSecret(c, "rendered-config")
	if string(secret.Data[ConfigFileName]) != "rendered-config" {
		t.Errorf("config secret content mismatch")
	}
	cm := BuildDynamicConfigMap(c, "")
	if cm.Data[DynamicConfigFileName] != "{}\n" {
		t.Errorf("empty dynamic config should default to {}")
	}
	if ConfigHash("a") == ConfigHash("b") {
		t.Errorf("different content should hash differently")
	}
	h1 := ConfigHash("stable")
	h2 := ConfigHash("stable")
	if h1 != h2 {
		t.Errorf("hash must be stable")
	}
}

func TestBuildCertificates(t *testing.T) {
	c := builderCluster()
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{
		Provider:  "cert-manager",
		IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"},
		Frontend:  &temporalv1alpha1.FrontendMTLSSpec{DNSNames: []string{"temporal.example.com"}},
	}

	internode := BuildInternodeCertificate(c)
	if internode.Name != "tc-internode" || internode.Spec.SecretName != "tc-internode" {
		t.Errorf("unexpected internode cert: %s/%s", internode.Name, internode.Spec.SecretName)
	}
	if internode.Spec.IssuerRef.Name != "ca" {
		t.Errorf("unexpected issuer %q", internode.Spec.IssuerRef.Name)
	}
	if len(internode.Spec.DNSNames) == 0 {
		t.Errorf("expected internode DNS names")
	}

	frontend := BuildFrontendCertificate(c)
	if !slices.Contains(frontend.Spec.DNSNames, "temporal.example.com") {
		t.Errorf("expected user DNS name in frontend cert: %v", frontend.Spec.DNSNames)
	}
}

func TestBuildClientCertificate(t *testing.T) {
	c := builderCluster()
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{
		Provider:  "cert-manager",
		IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"},
	}
	cc := &temporalv1alpha1.TemporalClusterClient{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "ns"},
		Spec:       temporalv1alpha1.TemporalClusterClientSpec{SecretName: "worker-creds"},
	}
	cert := BuildClientCertificate(cc, c)
	if cert.Spec.SecretName != "worker-creds" {
		t.Errorf("unexpected secret name %q", cert.Spec.SecretName)
	}
	if len(cert.Spec.Usages) != 1 || cert.Spec.Usages[0] != "client auth" {
		t.Errorf("expected client auth usage, got %v", cert.Spec.Usages)
	}
}

func TestBuildUI(t *testing.T) {
	c := builderCluster()
	c.Spec.UI = &temporalv1alpha1.UISpec{Enabled: true, Version: "2.34.0"}

	dep := BuildUIDeployment(c)
	if dep.Spec.Template.Spec.Containers[0].Image != "temporalio/ui:2.34.0" {
		t.Errorf("unexpected UI image %q", dep.Spec.Template.Spec.Containers[0].Image)
	}
	var hasAddr bool
	for _, e := range dep.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "TEMPORAL_ADDRESS" {
			hasAddr = true
		}
	}
	if !hasAddr {
		t.Errorf("expected TEMPORAL_ADDRESS env")
	}

	svc := BuildUIService(c)
	if svc.Spec.Ports[0].Port != 8080 {
		t.Errorf("expected UI service port 8080")
	}

	if BuildUIIngress(c) != nil {
		t.Errorf("expected no ingress when disabled")
	}
	c.Spec.UI.Ingress = &temporalv1alpha1.UIIngressSpec{Enabled: true, Host: "ui.example.com", IngressClassName: "nginx"}
	ing := BuildUIIngress(c)
	if ing == nil || ing.Spec.Rules[0].Host != "ui.example.com" {
		t.Errorf("expected ingress with host")
	}
	if ing.Spec.IngressClassName == nil || *ing.Spec.IngressClassName != "nginx" {
		t.Errorf("expected ingress class nginx")
	}
}

func TestBuildUIWithMTLS(t *testing.T) {
	c := builderCluster()
	c.Spec.UI = &temporalv1alpha1.UISpec{Enabled: true, Version: "2.34.0"}
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{Provider: "cert-manager", IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"}}

	cert := BuildUIClientCertificate(c)
	if cert.Spec.SecretName != "tc-ui-client" {
		t.Errorf("unexpected ui client secret %q", cert.Spec.SecretName)
	}
	dep := BuildUIDeployment(c)
	var hasVol bool
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "ui-client-certs" {
			hasVol = true
		}
	}
	if !hasVol {
		t.Errorf("expected ui-client-certs volume when mTLS enabled")
	}
}

func TestBuildServiceMonitor(t *testing.T) {
	c := builderCluster()
	c.Spec.Metrics = &temporalv1alpha1.MetricsSpec{
		Enabled:        true,
		ServiceMonitor: &temporalv1alpha1.ServiceMonitorSpec{Enabled: true, Labels: map[string]string{"release": "kps"}},
	}
	sm := BuildServiceMonitor(c)
	if sm.GetName() != "tc" || sm.GetNamespace() != "ns" {
		t.Errorf("unexpected SM metadata %s/%s", sm.GetNamespace(), sm.GetName())
	}
	if sm.GetLabels()["release"] != "kps" {
		t.Errorf("expected user label on ServiceMonitor")
	}
	if sm.GroupVersionKind() != ServiceMonitorGVK {
		t.Errorf("unexpected GVK %v", sm.GroupVersionKind())
	}
}
