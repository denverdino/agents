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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeyedLocker(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "same key returns same mutex",
			run: func(t *testing.T) {
				l := NewKeyedLocker()
				m1 := l.Get("key-a")
				m2 := l.Get("key-a")
				assert.Same(t, m1, m2)
			},
		},
		{
			name: "different keys return different mutexes",
			run: func(t *testing.T) {
				l := NewKeyedLocker()
				m1 := l.Get("key-a")
				m2 := l.Get("key-b")
				assert.NotSame(t, m1, m2)
			},
		},
		{
			name: "lock serializes concurrent access",
			run: func(t *testing.T) {
				l := NewKeyedLocker()
				const goroutines = 10

				var (
					concurrent atomic.Int32
					maxConc    atomic.Int32
					wg         sync.WaitGroup
				)

				wg.Add(goroutines)
				for i := 0; i < goroutines; i++ {
					go func() {
						defer wg.Done()
						mu := l.Get("shared")
						mu.Lock()
						cur := concurrent.Add(1)
						// Track peak concurrency.
						for {
							old := maxConc.Load()
							if cur <= old || maxConc.CompareAndSwap(old, cur) {
								break
							}
						}
						concurrent.Add(-1)
						mu.Unlock()
					}()
				}
				wg.Wait()

				assert.Equal(t, int32(1), maxConc.Load(), "expected at most 1 goroutine in the critical section at a time")
			},
		},
		{
			name: "delete removes the mutex",
			run: func(t *testing.T) {
				l := NewKeyedLocker()
				m1 := l.Get("key-a")
				l.Delete("key-a")
				m2 := l.Get("key-a")
				assert.NotSame(t, m1, m2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
