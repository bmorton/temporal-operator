// Licensed to Alexandre VILAIN under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Alexandre VILAIN licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package temporal

import (
	"context"
	"fmt"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	enumsv1 "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	"google.golang.org/grpc"
)

// searchAttributeTypes maps user-facing type names to Temporal IndexedValueType.
var searchAttributeTypes = map[string]enumsv1.IndexedValueType{
	"Text":        enumsv1.INDEXED_VALUE_TYPE_TEXT,
	"Keyword":     enumsv1.INDEXED_VALUE_TYPE_KEYWORD,
	"Int":         enumsv1.INDEXED_VALUE_TYPE_INT,
	"Double":      enumsv1.INDEXED_VALUE_TYPE_DOUBLE,
	"Bool":        enumsv1.INDEXED_VALUE_TYPE_BOOL,
	"DateTime":    enumsv1.INDEXED_VALUE_TYPE_DATETIME,
	"KeywordList": enumsv1.INDEXED_VALUE_TYPE_KEYWORD_LIST,
}

// searchAttributeTypeNames is the reverse mapping from IndexedValueType to string.
var searchAttributeTypeNames map[enumsv1.IndexedValueType]string

func init() {
	searchAttributeTypeNames = make(map[enumsv1.IndexedValueType]string, len(searchAttributeTypes))
	for name, val := range searchAttributeTypes {
		searchAttributeTypeNames[val] = name
	}
}

// SearchAttributeTypeFromString converts a user-facing type name to its IndexedValueType.
func SearchAttributeTypeFromString(s string) (enumsv1.IndexedValueType, error) {
	t, ok := searchAttributeTypes[s]
	if !ok {
		return enumsv1.INDEXED_VALUE_TYPE_UNSPECIFIED, fmt.Errorf("invalid search attribute type %q: valid types are Text, Keyword, Int, Double, Bool, DateTime, KeywordList", s)
	}
	return t, nil
}

// SearchAttributeTypeToString converts an IndexedValueType to its user-facing name.
func SearchAttributeTypeToString(t enumsv1.IndexedValueType) (string, error) {
	name, ok := searchAttributeTypeNames[t]
	if !ok {
		return "", fmt.Errorf("unknown IndexedValueType: %v", t)
	}
	return name, nil
}

// OperatorServiceClient is an interface for the Temporal OperatorService gRPC methods
// needed by search attribute reconciliation. It is satisfied by the client returned
// from temporalclient.Client.OperatorService().
type OperatorServiceClient interface {
	ListSearchAttributes(ctx context.Context, in *operatorservice.ListSearchAttributesRequest, opts ...grpc.CallOption) (*operatorservice.ListSearchAttributesResponse, error)
	AddSearchAttributes(ctx context.Context, in *operatorservice.AddSearchAttributesRequest, opts ...grpc.CallOption) (*operatorservice.AddSearchAttributesResponse, error)
	RemoveSearchAttributes(ctx context.Context, in *operatorservice.RemoveSearchAttributesRequest, opts ...grpc.CallOption) (*operatorservice.RemoveSearchAttributesResponse, error)
}

// ReconcileSearchAttributes ensures the custom search attributes on the Temporal server
// match the desired state declared in the TemporalNamespace spec.
func ReconcileSearchAttributes(ctx context.Context, operatorSvc OperatorServiceClient, namespace *v1beta1.TemporalNamespace) error {
	// 1. List current custom attributes from server.
	listResp, err := operatorSvc.ListSearchAttributes(ctx, &operatorservice.ListSearchAttributesRequest{
		Namespace: namespace.GetName(),
	})
	if err != nil {
		return fmt.Errorf("listing search attributes: %w", err)
	}

	existing := listResp.GetCustomAttributes()

	// 2. Parse desired spec into map[string]IndexedValueType.
	desired := make(map[string]enumsv1.IndexedValueType, len(namespace.Spec.CustomSearchAttributes))
	for name, typeStr := range namespace.Spec.CustomSearchAttributes {
		t, err := SearchAttributeTypeFromString(typeStr)
		if err != nil {
			return fmt.Errorf("search attribute %q: %w", name, err)
		}
		desired[name] = t
	}

	// 3. Detect type mismatches and compute adds.
	toAdd := make(map[string]enumsv1.IndexedValueType)
	for name, desiredType := range desired {
		existingType, exists := existing[name]
		if exists {
			if existingType != desiredType {
				existingTypeName, _ := SearchAttributeTypeToString(existingType)
				desiredTypeName, _ := SearchAttributeTypeToString(desiredType)
				return fmt.Errorf("search attribute %q has type %s on server but %s in spec; Temporal does not allow type changes", name, existingTypeName, desiredTypeName)
			}
			// Already exists with correct type, nothing to do.
			continue
		}
		toAdd[name] = desiredType
	}

	// 4. Compute removals (only if AllowSearchAttributeDeletion is true).
	var toRemove []string
	if namespace.Spec.AllowSearchAttributeDeletion {
		for name := range existing {
			if _, inSpec := desired[name]; !inSpec {
				toRemove = append(toRemove, name)
			}
		}
	}

	// 5. Add new attributes.
	if len(toAdd) > 0 {
		_, err := operatorSvc.AddSearchAttributes(ctx, &operatorservice.AddSearchAttributesRequest{
			SearchAttributes: toAdd,
			Namespace:        namespace.GetName(),
		})
		if err != nil {
			return fmt.Errorf("adding search attributes: %w", err)
		}
	}

	// 6. Remove stale attributes.
	if len(toRemove) > 0 {
		_, err := operatorSvc.RemoveSearchAttributes(ctx, &operatorservice.RemoveSearchAttributesRequest{
			SearchAttributes: toRemove,
			Namespace:        namespace.GetName(),
		})
		if err != nil {
			return fmt.Errorf("removing search attributes: %w", err)
		}
	}

	return nil
}
