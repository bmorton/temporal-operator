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

// Conversion / hub-and-spoke notes:
//
// v1alpha1 is currently the one and only API version, and is marked as the
// storage version (see the +kubebuilder:storageversion markers on the root
// types). When a v1beta1 is introduced, v1alpha1 will become the conversion
// "hub": all spoke versions convert to and from it, and conversion webhooks
// will be wired in here (via the conversion.Convertible / conversion.Hub
// interfaces from sigs.k8s.io/controller-runtime). Keeping this groundwork
// explicit now ensures the storage version is unambiguous and that adding a
// new version later is a localized change.
package v1alpha1
