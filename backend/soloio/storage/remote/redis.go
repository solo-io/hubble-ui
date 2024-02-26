package remote

import (
	"bytes"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/solo-io/skv2/pkg/ezkube"
	"github.com/solo-io/skv2/pkg/resource"
	"k8s.io/apimachinery/pkg/runtime"

	pkgresource "github.com/cilium/hubble-ui/backend/soloio/resource"
)

type redisPersistenceClient struct {
	client redis.UniversalClient
	scheme *runtime.Scheme

	// Common cache behavior
	*commonCache

	// streamId represents the stream position from which to read.
	// If empty we need to read all.
	streamId string

	keyBuilder *bytes.Buffer

	options Options
}

type Options struct {
	// Determines how often diffs are read from redis, default to time.Second/4
	RedisWatchRateLimit time.Duration
	// How long the redis stream is, default to 64
	RedisStreamMaxLen int
}

// commonCache represents the shared functionality between all cache impls
type commonCache struct {
	snapshots FullSnapshotManager

	handlerLock sync.RWMutex
	handlers    []EventHandlerFunc
}

type FullSnapshotManager interface {
	pkgresource.SnapshotManager

	Len() int

	Read(f func(snap resource.ClusterSnapshot))
	Write(f func(snap resource.ClusterSnapshot) error) error

	// submit a delta - this updates the snapshot and applies the delta to all delta trackers
	AddClusterDeltas([]*pkgresource.DeltaClusterSnapshot)
}

type EventHandlerFunc = func(id ezkube.ClusterResourceId)
