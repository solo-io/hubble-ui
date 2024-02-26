package ns_watcher

import (
	"context"
	"sync"

	"github.com/solo-io/skv2/pkg/ezkube"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cilium/hubble-ui/backend/internal/ns_watcher/common"
	"github.com/cilium/hubble-ui/backend/soloio/storage/remote"
)

var _ NSWatcherInterface = (*Watcher)(nil)

type Watcher struct {
	reader remote.Reader

	clients  map[chan<- struct{}]struct{}
	maplock  sync.RWMutex
	stop     chan struct{}
	stopOnce sync.Once
	events   chan *common.NSEvent
}

func NewSolo(r remote.Reader) (*Watcher, error) {
	w := &Watcher{
		reader:   r,
		clients:  map[chan<- struct{}]struct{}{},
		stop:     make(chan struct{}),
		stopOnce: sync.Once{},
	}
	r.Subscribe(w.trigger)
	return w, nil
}

func (w *Watcher) trigger(ezkube.ClusterResourceId) {
	w.maplock.RLock()
	defer w.maplock.RUnlock()

	for c := range w.clients {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}

func (w *Watcher) NSEvents() chan *common.NSEvent {
	if w.events == nil {
		w.events = make(chan *common.NSEvent)
	}

	return w.events
}

func (w *Watcher) Run(ctx context.Context) {
	w.runNSWatcher(ctx)
}

func (w *Watcher) runNSWatcher(ctx context.Context) {
	signal := make(chan struct{}, 1)
	w.maplock.Lock()
	w.clients[signal] = struct{}{}
	w.maplock.Unlock()

	defer func() {
		w.maplock.Lock()
		delete(w.clients, signal)
		w.maplock.Unlock()
	}()

	// make sure we get the first snapshot, but signaling the channel
	// it has capacity 1, so we don't block
	signal <- struct{}{}

	nsGvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}

	tracker := w.reader.TrackDeltas()
	// ignore delta as its always nil on first call
	for {
		select {
		case <-signal:
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		}

		snap, delta := tracker.DeltaSnapshot(func(GVK schema.GroupVersionKind) bool {
			return GVK == nsGvk
		})

		nsSet := make(map[string]struct{})

		for cluster := range snap {
			dsnap := delta.Snapshots()[cluster]
			if dsnap != nil {
				for k := range dsnap.GetUpdated()[nsGvk] {
					nsSet[k.Name] = struct{}{}
				}
			} else {
				snap.ForEachObject(func(snapCluster string, gvk schema.GroupVersionKind, obj client.Object) {
					if ns, ok := obj.(*v1.Namespace); ok && cluster == snapCluster {
						nsSet[ns.Name] = struct{}{}
					}
				})
			}
		}

		for ns := range nsSet {
			w.events <- &common.NSEvent{
				K8sNamespace: &v1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: ns,
					},
				},
			}
		}
	}
}

func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		if w.stop == nil {
			return
		}

		close(w.stop)
	})
}

func (w *Watcher) Errors() chan error {
	return nil
}
