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
	"fmt"
	"strconv"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

const (
	inspectorJobBackoffLimit   int32 = 0
	inspectorJobTTLAfterFinish int32 = 300
)

// InspectorJobParams describes the parameters for building an inspector Job.
type InspectorJobParams struct {
	// Cluster is the owning TemporalCluster.
	Cluster *temporalv1alpha1.TemporalCluster
	// Store identifies which logical store to inspect.
	Store SchemaStore
	// SQLSpec is the SQL datastore spec for the target store.
	SQLSpec *temporalv1alpha1.SQLDatastoreSpec
	// OperatorImage is the operator image to use for the inspect container.
	OperatorImage string
}

// InspectorJobName returns the deterministic name for an inspector Job.
func InspectorJobName(clusterName string, store SchemaStore) string {
	return fmt.Sprintf("%s-inspect-%s", clusterName, store)
}

// BuildInspectorJob builds a batch/v1 Job that runs the operator's "inspect"
// subcommand to probe a SQL store and read its schema version. The Job uses
// Azure Workload Identity for passwordless authentication, with the token
// wiring applied via ApplyAzureSchemaWorkloadIdentity. The caller is
// responsible for setting the controller owner reference on the returned Job.
func BuildInspectorJob(params InspectorJobParams) *batchv1.Job {
	spec := params.SQLSpec
	name := InspectorJobName(params.Cluster.Name, params.Store)
	backoff := inspectorJobBackoffLimit
	ttl := inspectorJobTTLAfterFinish

	labels := map[string]string{
		"app.kubernetes.io/name":       "temporal",
		"app.kubernetes.io/instance":   params.Cluster.Name,
		"app.kubernetes.io/component":  "inspector",
		"app.kubernetes.io/managed-by": "temporal-operator",
		"temporal.bmor10.com/store":    string(params.Store),
	}

	// Build the args for the inspect subcommand
	args := []string{
		"inspect",
		"--host", spec.Host,
		"--port", strconv.Itoa(int(spec.Port)),
		"--db", spec.Database,
		"--user", spec.User,
		"--plugin", spec.PluginName,
		"--password-file", AzureTokenFile,
	}

	// Append --tls when TLS is enabled
	if spec.TLS != nil && spec.TLS.Enabled {
		args = append(args, "--tls")
	}

	// Build the main inspect container
	inspectContainer := corev1.Container{
		Name:                     "inspect",
		Image:                    params.OperatorImage,
		Args:                     args,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	// Build the pod template with the inspect container
	template := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels},
		Spec: corev1.PodSpec{
			RestartPolicy:    corev1.RestartPolicyNever,
			ImagePullSecrets: params.Cluster.Spec.ImagePullSecrets,
			Containers: []corev1.Container{
				inspectContainer,
			},
		},
	}

	// Apply Azure Workload Identity configuration (ServiceAccount, WI label,
	// azure-token initContainer, /azure volume + mount)
	ApplyAzureSchemaWorkloadIdentity(&template.ObjectMeta, &template.Spec, params.Cluster, "inspect")

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "Job"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: params.Cluster.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template:                template,
		},
	}
}
