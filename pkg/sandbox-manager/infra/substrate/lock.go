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

import "sync"

// KeyedLocker provides per-key mutexes to serialize lifecycle operations on the same actor.
type KeyedLocker struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewKeyedLocker() *KeyedLocker {
	return &KeyedLocker{
		locks: make(map[string]*sync.Mutex),
	}
}

// Get returns a mutex for the given key, creating one if it doesn't exist.
func (l *KeyedLocker) Get(key string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, ok := l.locks[key]
	if !ok {
		m = &sync.Mutex{}
		l.locks[key] = m
	}
	return m
}

// Delete removes the mutex for the given key. Call only when no goroutine holds or waits on it.
func (l *KeyedLocker) Delete(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.locks, key)
}
