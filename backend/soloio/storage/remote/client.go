package remote

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/solo-io/skv2/pkg/ezkube"
	sk_resource "github.com/solo-io/skv2/pkg/resource"

	"github.com/cilium/hubble-ui/backend/soloio/resource"
)

type Storage interface {
	Reader
	Writer
}

type EventHandlerFunc = func(id ezkube.ClusterResourceId)

type (
	GVKSelectorFunc = sk_resource.GVKSelectorFunc
	ClusterSnapshot = sk_resource.ClusterSnapshot
	Reader          interface {
		// I really wanted to have this interface
		// implement resource.SnapshotManager; but gomock doesn't let me
		// so instead i copy-paste it in, and verify that it implements it
		// right after the interface
		ShallowCopySnapshots(selectors ...GVKSelectorFunc) ClusterSnapshot
		// get a delta tracker to track deltas
		TrackDeltas() resource.DeltaTracker
		// Subscribe to persistence watch events with the given handler
		Subscribe(handler EventHandlerFunc)
	}
)

var _ resource.SnapshotManager = Reader(nil)

type CacheReaderLister interface {
	// Get a specific object from this reader.
	Get(
		ctx context.Context,
		cluster string,
		GVK schema.GroupVersionKind,
		namespace, name string,
	) sk_resource.TypedObject

	// List all objects of the given type from this reader.
	List(
		ctx context.Context,
		cluster string,
		GVK schema.GroupVersionKind,
		namespace string,
	) []sk_resource.TypedObject
}

type Writer interface {
	// Write the ClusterSnapshot to persistent storage
	Write(ctx context.Context, cs sk_resource.ClusterSnapshot) error

	// Clean should clear out any stale data from the cache.
	// This should only be called by an elected leader.
	Clean(ctx context.Context, activeClusters []string) error
}
