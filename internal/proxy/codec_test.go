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

package proxy

import (
	"bytes"
	"testing"
)

func TestRawCodecRoundTrip(t *testing.T) {
	c := RawCodec{}
	in := &Frame{Payload: []byte("hello-temporal")}
	b, err := c.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := &Frame{}
	if err := c.Unmarshal(b, out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !bytes.Equal(out.Payload, in.Payload) {
		t.Fatalf("payload = %q, want %q", out.Payload, in.Payload)
	}
	if c.Name() != "proxy-raw" {
		t.Fatalf("name = %q, want proxy-raw", c.Name())
	}
}
