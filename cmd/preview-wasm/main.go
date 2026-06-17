//go:build js && wasm

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

// Command preview-wasm exposes the operator's object planner to the browser. It
// registers a global temporalPreview(kind, yaml) function that returns a JSON
// string {resources:[{kind,apiVersion,name,namespace,phase,yaml}], error}
// describing every object the operator would create.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"syscall/js"

	corev1 "k8s.io/api/core/v1"
	apiyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/plan"
	webhookv1alpha1 "github.com/bmorton/temporal-operator/internal/webhook/v1alpha1"
)

type previewResource struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Phase      string `json:"phase"`
	YAML       string `json:"yaml"`
}

type previewResult struct {
	Resources []previewResource `json:"resources"`
	Error     string            `json:"error,omitempty"`
}

func ok(objs []previewResource) string {
	if objs == nil {
		objs = []previewResource{}
	}
	b, _ := json.Marshal(previewResult{Resources: objs})
	return string(b)
}

func fail(format string, args ...any) string {
	b, _ := json.Marshal(previewResult{
		Resources: []previewResource{},
		Error:     fmt.Sprintf(format, args...),
	})
	return string(b)
}

// previewTemporalCluster handles the one fully-wired kind. Additional kinds are
// added by extending the switch in temporalPreview.
func previewTemporalCluster(yamlSrc string) string {
	cluster, err := decodeTemporalCluster(yamlSrc)
	if err != nil {
		return fail("%s", err.Error())
	}
	if cluster.Namespace == "" {
		cluster.Namespace = "default"
	}

	ctx := context.Background()
	defaulter := &webhookv1alpha1.TemporalClusterCustomDefaulter{}
	if err := defaulter.Default(ctx, cluster); err != nil {
		return fail("defaulting failed: %v", err)
	}

	validator := &webhookv1alpha1.TemporalClusterCustomValidator{}
	if _, err := validator.ValidateCreate(ctx, cluster); err != nil {
		return fail("validation failed: %v", err)
	}

	planned, err := plan.PlanFromSpec(cluster)
	if err != nil {
		return fail("%s", err.Error())
	}

	objs := make([]previewResource, 0, len(planned))
	for _, p := range planned {
		rendered, err := renderObject(p.Object)
		if err != nil {
			return fail("rendering %s: %v", p.Object.GetName(), err)
		}
		gvk := p.Object.GetObjectKind().GroupVersionKind()
		objs = append(objs, previewResource{
			Kind:       gvk.Kind,
			APIVersion: gvk.GroupVersion().String(),
			Name:       p.Object.GetName(),
			Namespace:  p.Object.GetNamespace(),
			Phase:      string(p.Phase),
			YAML:       rendered,
		})
	}
	return ok(objs)
}

// decodeTemporalCluster scans the (possibly multi-document) YAML input and
// returns the first TemporalCluster it finds. This lets users paste a whole
// manifest file that bundles other resources (Secrets, ServiceAccounts, etc.)
// alongside the cluster.
func decodeTemporalCluster(yamlSrc string) (*temporalv1alpha1.TemporalCluster, error) {
	if strings.TrimSpace(yamlSrc) == "" {
		return nil, fmt.Errorf("no input: paste a TemporalCluster manifest")
	}
	dec := apiyaml.NewYAMLOrJSONDecoder(strings.NewReader(yamlSrc), 4096)
	sawDocument := false
	for {
		var cluster temporalv1alpha1.TemporalCluster
		if err := dec.Decode(&cluster); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("invalid YAML: %v", err)
		}
		sawDocument = true
		if cluster.Kind == "TemporalCluster" {
			return &cluster, nil
		}
	}
	if !sawDocument {
		return nil, fmt.Errorf("no input: paste a TemporalCluster manifest")
	}
	return nil, fmt.Errorf("no TemporalCluster document found in the input")
}

// renderObject marshals an object to YAML, decoding Secret data to readable text
// so the rendered Temporal config is visible instead of base64.
func renderObject(obj client.Object) (string, error) {
	if secret, ok := obj.(*corev1.Secret); ok && len(secret.Data) > 0 {
		if secret.StringData == nil {
			secret.StringData = map[string]string{}
		}
		for k, v := range secret.Data {
			secret.StringData[k] = string(v)
		}
		secret.Data = nil
	}
	b, err := yaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func temporalPreview(_ js.Value, args []js.Value) any {
	if len(args) < 2 {
		return fail("temporalPreview(kind, yaml) requires two arguments")
	}
	kind := args[0].String()
	src := args[1].String()
	switch kind {
	case "TemporalCluster":
		return previewTemporalCluster(src)
	default:
		return fail("kind %q is not supported yet", kind)
	}
}

func main() {
	js.Global().Set("temporalPreview", js.FuncOf(temporalPreview))
	select {} // keep the Go runtime alive for callbacks
}
