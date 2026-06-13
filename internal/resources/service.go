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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// HeadlessServiceName returns the name of a service's headless membership Service.
func HeadlessServiceName(clusterName, component string) string {
	return clusterName + "-" + component + "-headless"
}

// FrontendServiceName returns the name of the cluster's frontend client Service.
func FrontendServiceName(clusterName string) string {
	return clusterName + "-frontend"
}

// BuildHeadlessService builds the headless Service used for Ringpop membership.
func BuildHeadlessService(cluster *temporalv1alpha1.TemporalCluster, svc ServiceInfo) *corev1.Service {
	ports := []corev1.ServicePort{
		{Name: "grpc", Port: svc.Ports.GRPCPort, TargetPort: intstrFromInt(svc.Ports.GRPCPort)},
		{Name: "membership", Port: svc.Ports.MembershipPort, TargetPort: intstrFromInt(svc.Ports.MembershipPort)},
	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      HeadlessServiceName(cluster.Name, svc.Name),
			Namespace: cluster.Namespace,
			Labels:    StandardLabels(cluster, svc.Name),
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                "None",
			PublishNotReadyAddresses: true,
			Selector:                 SelectorLabels(cluster, svc.Name),
			Ports:                    ports,
		},
	}
}

// BuildFrontendService builds the regular ClusterIP Service that clients use to
// reach the frontend gRPC (and HTTP) endpoints.
func BuildFrontendService(cluster *temporalv1alpha1.TemporalCluster, frontend ServiceInfo) *corev1.Service {
	ports := []corev1.ServicePort{
		{Name: "grpc", Port: frontend.Ports.GRPCPort, TargetPort: intstrFromInt(frontend.Ports.GRPCPort)},
	}
	if frontend.Ports.HTTPPort != 0 {
		ports = append(ports, corev1.ServicePort{
			Name: "http", Port: frontend.Ports.HTTPPort, TargetPort: intstrFromInt(frontend.Ports.HTTPPort),
		})
	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      FrontendServiceName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    StandardLabels(cluster, ServiceFrontend),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: SelectorLabels(cluster, ServiceFrontend),
			Ports:    ports,
		},
	}
}
