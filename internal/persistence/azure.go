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

package persistence

import (
	"context"
	"fmt"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// tokenProvider obtains a bearer token for the given scope. It abstracts Azure
// Workload Identity so the SQL backend can be unit-tested with a fake.
type tokenProvider interface {
	Token(ctx context.Context, scope string) (string, error)
}

// workloadIdentityTokenProvider obtains Microsoft Entra access tokens via Azure
// Workload Identity. The underlying credential reads the AZURE_* environment and
// the projected federated token that the Azure Workload Identity webhook injects
// into the operator pod. The SDK caches and refreshes tokens internally.
type workloadIdentityTokenProvider struct {
	once sync.Once
	cred *azidentity.WorkloadIdentityCredential
	err  error
}

func (p *workloadIdentityTokenProvider) Token(ctx context.Context, scope string) (string, error) {
	p.once.Do(func() {
		p.cred, p.err = azidentity.NewWorkloadIdentityCredential(nil)
	})
	if p.err != nil {
		return "", fmt.Errorf("creating workload identity credential: %w", p.err)
	}
	tok, err := p.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{scope}})
	if err != nil {
		return "", fmt.Errorf("acquiring entra token: %w", err)
	}
	return tok.Token, nil
}

// defaultTokenProvider is the process-wide Azure Workload Identity provider.
var defaultTokenProvider tokenProvider = &workloadIdentityTokenProvider{}
