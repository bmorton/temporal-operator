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
	"fmt"

	"google.golang.org/grpc/encoding"
)

// CodecName is the registered name of the raw passthrough codec.
const CodecName = "proxy-raw"

// Frame is an opaque gRPC message: the raw wire bytes, never deserialized.
type Frame struct {
	Payload []byte
}

// RawCodec forwards message bytes verbatim so the proxy can relay any method
// without depending on its protobuf types.
type RawCodec struct{}

func (RawCodec) Marshal(v any) ([]byte, error) {
	f, ok := v.(*Frame)
	if !ok {
		return nil, fmt.Errorf("proxy: RawCodec expects *Frame, got %T", v)
	}
	return f.Payload, nil
}

func (RawCodec) Unmarshal(data []byte, v any) error {
	f, ok := v.(*Frame)
	if !ok {
		return fmt.Errorf("proxy: RawCodec expects *Frame, got %T", v)
	}
	f.Payload = data
	return nil
}

func (RawCodec) Name() string { return CodecName }

func init() {
	encoding.RegisterCodec(RawCodec{})
}
