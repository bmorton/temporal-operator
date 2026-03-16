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
	"testing"

	"github.com/alexandrevilain/temporal-operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	enumsv1 "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockOperatorServiceClient is a mock for testing.
type mockOperatorServiceClient struct {
	listResponse *operatorservice.ListSearchAttributesResponse
	listError    error
	addError     error
	removeError  error

	addCalled    bool
	removeCalled bool
	addRequest   *operatorservice.AddSearchAttributesRequest
	removeRequest *operatorservice.RemoveSearchAttributesRequest
}

func (m *mockOperatorServiceClient) ListSearchAttributes(_ context.Context, _ *operatorservice.ListSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.ListSearchAttributesResponse, error) {
	return m.listResponse, m.listError
}

func (m *mockOperatorServiceClient) AddSearchAttributes(_ context.Context, req *operatorservice.AddSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.AddSearchAttributesResponse, error) {
	m.addCalled = true
	m.addRequest = req
	return &operatorservice.AddSearchAttributesResponse{}, m.addError
}

func (m *mockOperatorServiceClient) RemoveSearchAttributes(_ context.Context, req *operatorservice.RemoveSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.RemoveSearchAttributesResponse, error) {
	m.removeCalled = true
	m.removeRequest = req
	return &operatorservice.RemoveSearchAttributesResponse{}, m.removeError
}

func TestSearchAttributeTypeFromString(t *testing.T) {
	tests := map[string]struct {
		input    string
		expected enumsv1.IndexedValueType
		wantErr  bool
	}{
		"Text":        {input: "Text", expected: enumsv1.INDEXED_VALUE_TYPE_TEXT},
		"Keyword":     {input: "Keyword", expected: enumsv1.INDEXED_VALUE_TYPE_KEYWORD},
		"Int":         {input: "Int", expected: enumsv1.INDEXED_VALUE_TYPE_INT},
		"Double":      {input: "Double", expected: enumsv1.INDEXED_VALUE_TYPE_DOUBLE},
		"Bool":        {input: "Bool", expected: enumsv1.INDEXED_VALUE_TYPE_BOOL},
		"DateTime":    {input: "DateTime", expected: enumsv1.INDEXED_VALUE_TYPE_DATETIME},
		"KeywordList": {input: "KeywordList", expected: enumsv1.INDEXED_VALUE_TYPE_KEYWORD_LIST},
		"invalid":     {input: "invalid", wantErr: true},
		"empty":       {input: "", wantErr: true},
		"lowercase":   {input: "text", wantErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := SearchAttributeTypeFromString(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSearchAttributeTypeToString(t *testing.T) {
	tests := map[string]struct {
		input    enumsv1.IndexedValueType
		expected string
		wantErr  bool
	}{
		"Text":        {input: enumsv1.INDEXED_VALUE_TYPE_TEXT, expected: "Text"},
		"Keyword":     {input: enumsv1.INDEXED_VALUE_TYPE_KEYWORD, expected: "Keyword"},
		"Int":         {input: enumsv1.INDEXED_VALUE_TYPE_INT, expected: "Int"},
		"Double":      {input: enumsv1.INDEXED_VALUE_TYPE_DOUBLE, expected: "Double"},
		"Bool":        {input: enumsv1.INDEXED_VALUE_TYPE_BOOL, expected: "Bool"},
		"DateTime":    {input: enumsv1.INDEXED_VALUE_TYPE_DATETIME, expected: "DateTime"},
		"KeywordList": {input: enumsv1.INDEXED_VALUE_TYPE_KEYWORD_LIST, expected: "KeywordList"},
		"unspecified": {input: enumsv1.INDEXED_VALUE_TYPE_UNSPECIFIED, wantErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := SearchAttributeTypeToString(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func newNamespace(name string, attrs map[string]string, allowDeletion bool) *v1beta1.TemporalNamespace {
	return &v1beta1.TemporalNamespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1beta1.TemporalNamespaceSpec{
			CustomSearchAttributes:       attrs,
			AllowSearchAttributeDeletion: allowDeletion,
		},
	}
}

func TestReconcileSearchAttributes(t *testing.T) {
	ctx := context.Background()

	t.Run("add new attributes", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enumsv1.IndexedValueType{},
			},
		}
		ns := newNamespace("test-ns", map[string]string{
			"CustomerId": "Keyword",
			"OrderTotal": "Double",
		}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		require.NoError(t, err)
		assert.True(t, mock.addCalled)
		assert.False(t, mock.removeCalled)
		assert.Equal(t, map[string]enumsv1.IndexedValueType{
			"CustomerId": enumsv1.INDEXED_VALUE_TYPE_KEYWORD,
			"OrderTotal": enumsv1.INDEXED_VALUE_TYPE_DOUBLE,
		}, mock.addRequest.SearchAttributes)
		assert.Equal(t, "test-ns", mock.addRequest.Namespace)
	})

	t.Run("remove stale with flag on", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enumsv1.IndexedValueType{
					"OldAttr": enumsv1.INDEXED_VALUE_TYPE_TEXT,
				},
			},
		}
		ns := newNamespace("test-ns", map[string]string{}, true)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		require.NoError(t, err)
		assert.False(t, mock.addCalled)
		assert.True(t, mock.removeCalled)
		assert.Equal(t, []string{"OldAttr"}, mock.removeRequest.SearchAttributes)
	})

	t.Run("no removal with flag off", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enumsv1.IndexedValueType{
					"OldAttr": enumsv1.INDEXED_VALUE_TYPE_TEXT,
				},
			},
		}
		ns := newNamespace("test-ns", map[string]string{}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		require.NoError(t, err)
		assert.False(t, mock.addCalled)
		assert.False(t, mock.removeCalled)
	})

	t.Run("no changes when spec matches server", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enumsv1.IndexedValueType{
					"CustomerId": enumsv1.INDEXED_VALUE_TYPE_KEYWORD,
				},
			},
		}
		ns := newNamespace("test-ns", map[string]string{
			"CustomerId": "Keyword",
		}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		require.NoError(t, err)
		assert.False(t, mock.addCalled)
		assert.False(t, mock.removeCalled)
	})

	t.Run("mixed add and remove", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enumsv1.IndexedValueType{
					"Existing": enumsv1.INDEXED_VALUE_TYPE_KEYWORD,
					"OldAttr":  enumsv1.INDEXED_VALUE_TYPE_TEXT,
				},
			},
		}
		ns := newNamespace("test-ns", map[string]string{
			"Existing": "Keyword",
			"NewAttr":  "Bool",
		}, true)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		require.NoError(t, err)
		assert.True(t, mock.addCalled)
		assert.True(t, mock.removeCalled)
		assert.Equal(t, map[string]enumsv1.IndexedValueType{
			"NewAttr": enumsv1.INDEXED_VALUE_TYPE_BOOL,
		}, mock.addRequest.SearchAttributes)
		assert.Equal(t, []string{"OldAttr"}, mock.removeRequest.SearchAttributes)
	})

	t.Run("type mismatch returns error", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enumsv1.IndexedValueType{
					"CustomerId": enumsv1.INDEXED_VALUE_TYPE_TEXT,
				},
			},
		}
		ns := newNamespace("test-ns", map[string]string{
			"CustomerId": "Keyword",
		}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not allow type changes")
		assert.False(t, mock.addCalled)
		assert.False(t, mock.removeCalled)
	})

	t.Run("invalid type string returns error", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enumsv1.IndexedValueType{},
			},
		}
		ns := newNamespace("test-ns", map[string]string{
			"CustomerId": "InvalidType",
		}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid search attribute type")
		assert.False(t, mock.addCalled)
	})

	t.Run("list error propagated", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listError: fmt.Errorf("connection refused"),
		}
		ns := newNamespace("test-ns", map[string]string{
			"CustomerId": "Keyword",
		}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "connection refused")
	})

	t.Run("add error propagated", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enumsv1.IndexedValueType{},
			},
			addError: fmt.Errorf("server error"),
		}
		ns := newNamespace("test-ns", map[string]string{
			"CustomerId": "Keyword",
		}, false)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "server error")
	})

	t.Run("remove error propagated", func(t *testing.T) {
		mock := &mockOperatorServiceClient{
			listResponse: &operatorservice.ListSearchAttributesResponse{
				CustomAttributes: map[string]enumsv1.IndexedValueType{
					"OldAttr": enumsv1.INDEXED_VALUE_TYPE_TEXT,
				},
			},
			removeError: fmt.Errorf("removal failed"),
		}
		ns := newNamespace("test-ns", map[string]string{}, true)

		err := ReconcileSearchAttributes(ctx, mock, ns)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "removal failed")
	})
}
