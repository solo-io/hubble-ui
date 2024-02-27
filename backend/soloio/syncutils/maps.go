package syncutils

import (
	"sync"

	"golang.org/x/exp/maps"
)

// AtomicMap is a generic map type that is safe for concurrent access.
// It can be used as any normal map would.
type AtomicMap[K comparable, V any] struct {
	m    map[K]V
	lock sync.RWMutex
}

// Get the value for the given key in a thread safe manner.
func (a *AtomicMap[K, V]) Get(key K) (V, bool) {
	if a == nil {
		var val V
		return val, false
	}
	a.lock.RLock()
	defer a.lock.RUnlock()
	if a.m == nil {
		var val V
		return val, false
	}
	val, ok := a.m[key]
	return val, ok
}

// Set the value for the given key in a thread safe manner.
func (a *AtomicMap[K, V]) Set(key K, val V) {
	a.createIfDoesntExist()
	a.lock.Lock()
	defer a.lock.Unlock()
	a.m[key] = val
}

// Delete the value for the given key in a thread safe manner.
// If a value is present it will also return it
func (a *AtomicMap[K, V]) Delete(key K) (V, bool) {
	var (
		val V
		ok  bool
	)

	if a == nil {
		return val, false
	}
	if val, ok = a.Get(key); !ok {
		return val, ok
	}
	a.lock.Lock()
	defer a.lock.Unlock()
	delete(a.m, key)
	return val, true
}

// Return a filtered list of keys, does NOT modify original AtomicMap
func (a *AtomicMap[K, V]) Filter(filterFunc func(key K, value V) bool) []K {
	if a == nil {
		return []K{}
	}
	a.lock.Lock()
	defer a.lock.Unlock()
	var matchedKeys []K
	if a.m == nil {
		return []K{}
	}
	for k, v := range a.m {
		if filterFunc(k, v) {
			matchedKeys = append(matchedKeys, k)
		}
	}
	return matchedKeys
}

func (a *AtomicMap[K, V]) Range(fn func(key K, value V)) {
	if a == nil {
		return
	}
	a.lock.Lock()
	defer a.lock.Unlock()

	if a.m == nil {
		return
	}

	for k, v := range a.m {
		fn(k, v)
	}
}

// Get AtomicMap length
func (a *AtomicMap[K, V]) Length() int {
	if a == nil {
		return 0
	}
	a.lock.RLock()
	defer a.lock.RUnlock()
	if a.m == nil {
		return 0
	}
	return len(a.m)
}

func (a *AtomicMap[K, V]) Has(key K) bool {
	if a == nil {
		return false
	}
	a.lock.RLock()
	defer a.lock.RUnlock()
	if a.m == nil {
		return false
	}
	_, contained := a.m[key]
	return contained
}

func (a *AtomicMap[K, V]) Keys() []K {
	if a == nil {
		return []K{}
	}
	a.lock.RLock()
	defer a.lock.RUnlock()
	if a.m == nil {
		return []K{}
	}
	return maps.Keys(a.m)
}

// Update will pass the current value for the given key to the updateFn which should return a value
// that will be set as the new value for the given key. Useful when multiple routines
// need to read and write to a key as a single atomic operation.
// AtomicMap must be initialized before calling Update in multiple goroutines
func (a *AtomicMap[K, V]) Update(key K, updateFn func(val V) V) {
	a.lock.Lock()
	defer a.lock.Unlock()
	val := a.m[key]
	a.m[key] = updateFn(val)
}

// Initialize the AtomicMap
func (a *AtomicMap[K, V]) Initialize() {
	a.createIfDoesntExist()
}

// ensure that the map exists before attempting to access it.
func (a *AtomicMap[K, V]) createIfDoesntExist() {
	a.lock.RLock()
	if a.m == nil {
		a.lock.RUnlock()
		a.lock.Lock()
		a.m = make(map[K]V)
		a.lock.Unlock()
		return
	}
	a.lock.RUnlock()
}
