package remote

import (
	"context"
	"encoding/json"
	"sync"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/solo-io/go-utils/contextutils"
	"github.com/solo-io/skv2/pkg/ezkube"
	"github.com/solo-io/skv2/pkg/resource"

	pkgresource "github.com/cilium/hubble-ui/backend/soloio/resource"
)

// buildGvkMapFromRaw builds a piece of a resource.Snapshot using json blobs and a kubernetes scheme
func buildGvkMapFromRaw(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	scheme *runtime.Scheme,
	blobs map[string]string,
) (map[types.NamespacedName]resource.TypedObject, error) {
	gvkSnap := map[types.NamespacedName]resource.TypedObject{}

	newObj, err := scheme.New(gvk)
	if err != nil {
		return nil, err
	}

	for _, v := range blobs {
		clone := newObj.DeepCopyObject().(resource.TypedObject)
		if err := json.Unmarshal([]byte(v), clone); err != nil {
			contextutils.LoggerFrom(ctx).Errorw("could not unmarshal object", zap.Error(err))
			return nil, err
		}
		gvkSnap[types.NamespacedName{
			Namespace: clone.GetNamespace(),
			Name:      clone.GetName(),
		}] = clone
	}
	return gvkSnap, nil
}

func newCommonCache() *commonCache {
	return &commonCache{
		snapshots: NewFullSnapshotManager(),
	}
}

// commonCache represents the shared functionality between all cache impls
type commonCache struct {
	snapshots FullSnapshotManager

	handlerLock sync.RWMutex
	handlers    []EventHandlerFunc
}

func (c *commonCache) Clone(
	ctx context.Context,
	selectors ...resource.GVKSelectorFunc,
) resource.ClusterSnapshot {
	return c.snapshots.ShallowCopySnapshots(selectors...)
}

func (c *commonCache) TrackDeltas() pkgresource.DeltaTracker {
	return c.snapshots.TrackDeltas()
}

func (c *commonCache) ShallowCopySnapshots(
	selectors ...resource.GVKSelectorFunc,
) resource.ClusterSnapshot {
	return c.snapshots.ShallowCopySnapshots(selectors...)
}

func (c *commonCache) Get(
	ctx context.Context,
	cluster string,
	GVK schema.GroupVersionKind,
	namespace, name string,
) resource.TypedObject {
	var obj resource.TypedObject
	c.snapshots.Read(func(snapshot resource.ClusterSnapshot) {
		snap, ok := snapshot[cluster]
		if !ok {
			return
		}
		resources, ok := snap[GVK]
		if !ok {
			return
		}
		obj = resources[types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}]
	})
	return obj
}

func (c *commonCache) List(
	ctx context.Context,
	cluster string,
	GVK schema.GroupVersionKind,
	namespace string,
) []resource.TypedObject {
	var result []resource.TypedObject
	c.snapshots.Read(func(snapshot resource.ClusterSnapshot) {
		snap, ok := snapshot[cluster]
		if !ok {
			return
		}
		resources, ok := snap[GVK]
		if !ok {
			return
		}
		for _, obj := range resources {
			if obj.GetNamespace() == namespace {
				result = append(result, obj)
			}
		}
	})

	return result
}

func (c *commonCache) Subscribe(handler EventHandlerFunc) {
	c.handlerLock.Lock()
	defer c.handlerLock.Unlock()
	c.handlers = append(c.handlers, handler)
}

func (c *commonCache) signalHandlers(id ezkube.ClusterResourceId) {
	// Send these off in a go-routine so that they aren't blocking the lock above
	go func() {
		c.handlerLock.RLock()
		defer c.handlerLock.RUnlock()
		for _, handler := range c.handlers {
			handler(id)
		}
	}()
}
