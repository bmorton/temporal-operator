package plan

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func testCluster() *temporalv1alpha1.TemporalCluster {
	return &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tc", Namespace: "ns"},
		Spec:       temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1"},
	}
}

func kinds(objs []PlannedObject) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.Object.GetObjectKind().GroupVersionKind().Kind)
	}
	return out
}

func TestPlanMTLSDisabled(t *testing.T) {
	if got := PlanMTLS(testCluster()); len(got) != 0 {
		t.Errorf("expected no mTLS objects when disabled, got %v", kinds(got))
	}
}

func TestPlanMTLSEnabled(t *testing.T) {
	c := testCluster()
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{Provider: "cert-manager", IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"}}
	got := PlanMTLS(c)
	if len(got) != 2 {
		t.Fatalf("expected internode + frontend certs, got %d", len(got))
	}
	for _, o := range got {
		if o.Phase != PhaseMTLS {
			t.Errorf("expected PhaseMTLS, got %s", o.Phase)
		}
	}
}

func TestPlanUIDisabledAndEnabled(t *testing.T) {
	c := testCluster()
	if got := PlanUI(c); len(got) != 0 {
		t.Errorf("expected no UI objects when disabled, got %v", kinds(got))
	}
	c.Spec.UI = &temporalv1alpha1.UISpec{Enabled: true, Version: "2.34.0"}
	got := PlanUI(c)
	if len(got) != 2 {
		t.Fatalf("expected deployment + service, got %d (%v)", len(got), kinds(got))
	}
	c.Spec.UI.Ingress = &temporalv1alpha1.UIIngressSpec{Enabled: true, Host: "ui.example.com"}
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{Provider: "cert-manager", IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"}}
	got = PlanUI(c)
	if len(got) != 4 {
		t.Fatalf("expected cert+deployment+service+ingress, got %d (%v)", len(got), kinds(got))
	}
}

func TestPlanMonitoring(t *testing.T) {
	c := testCluster()
	if got := PlanMonitoring(c); len(got) != 0 {
		t.Errorf("expected no ServiceMonitor when disabled")
	}
	c.Spec.Metrics = &temporalv1alpha1.MetricsSpec{
		Enabled:        true,
		ServiceMonitor: &temporalv1alpha1.ServiceMonitorSpec{Enabled: true},
	}
	if got := PlanMonitoring(c); len(got) != 1 {
		t.Errorf("expected one ServiceMonitor, got %d", len(got))
	}
}
