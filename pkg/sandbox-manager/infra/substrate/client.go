/*
Copyright 2026.

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

package substrate

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	grpcinsecure "google.golang.org/grpc/credentials/insecure"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// SubstrateClient wraps the Substrate Control gRPC client.
type SubstrateClient struct {
	conn    *grpc.ClientConn
	control ateapipb.ControlClient
}

// NewSubstrateClient creates a new Substrate gRPC client connected to the given address.
// Addresses prefixed with "insecure://" use plaintext gRPC; all others use TLS
// with InsecureSkipVerify (the Substrate API server requires TLS).
func NewSubstrateClient(ctx context.Context, addr string) (*SubstrateClient, error) {
	var creds grpc.DialOption
	if strings.HasPrefix(addr, "insecure://") {
		addr = strings.TrimPrefix(addr, "insecure://")
		creds = grpc.WithTransportCredentials(grpcinsecure.NewCredentials())
	} else {
		creds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // prototype: skip cert verification for dev clusters
		}))
	}
	conn, err := grpc.NewClient(addr, creds)
	if err != nil {
		return nil, fmt.Errorf("dial substrate control at %s: %w", addr, err)
	}
	return &SubstrateClient{
		conn:    conn,
		control: ateapipb.NewControlClient(conn),
	}, nil
}

func (c *SubstrateClient) Control() ateapipb.ControlClient {
	return c.control
}

func (c *SubstrateClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
