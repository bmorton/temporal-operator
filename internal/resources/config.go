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
	"crypto/sha256"
	"encoding/hex"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

const (
	// ConfigFileName is the rendered server config file key/mount name.
	ConfigFileName = "config.yaml"
	// DynamicConfigFileName is the dynamic config file key/mount name.
	DynamicConfigFileName = "dynamic_config.yaml"
)

// ConfigSecretName returns the name of the Secret holding the rendered server
// config. The config is stored in a Secret (not a ConfigMap) because it embeds
// datastore credentials.
func ConfigSecretName(clusterName string) string {
	return clusterName + "-config"
}

// DynamicConfigMapName returns the name of the ConfigMap holding dynamic config.
func DynamicConfigMapName(clusterName string) string {
	return clusterName + "-dynamicconfig"
}

// ConfigHash returns a stable short hash of the rendered config content, used to
// trigger pod rollouts when the config changes.
func ConfigHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:16]
}

// BuildConfigSecret builds the Secret containing the rendered server config.
func BuildConfigSecret(cluster *temporalv1alpha1.TemporalCluster, rendered string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigSecretName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    StandardLabels(cluster, "config"),
		},
		Data: map[string][]byte{
			ConfigFileName: []byte(rendered),
		},
	}
}

// BuildDynamicConfigMap builds the ConfigMap containing dynamic config. When the
// rendered content is empty, an empty document is written so the mount always
// exists.
func BuildDynamicConfigMap(cluster *temporalv1alpha1.TemporalCluster, rendered string) *corev1.ConfigMap {
	if rendered == "" {
		rendered = "{}\n"
	}
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DynamicConfigMapName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    StandardLabels(cluster, "dynamicconfig"),
		},
		Data: map[string]string{
			DynamicConfigFileName: rendered,
		},
	}
}
