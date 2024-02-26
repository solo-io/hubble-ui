package remote

import (
	"sync"

	"github.com/solo-io/skv2/pkg/resource"

	pkgresource "github.com/cilium/hubble-ui/backend/soloio/resource"
)

type FullSnapshotManager interface {
	pkgresource.SnapshotManager

	Len() int

	Read(f func(snap resource.ClusterSnapshot))
	Write(f func(snap resource.ClusterSnapshot) error) error

	// submit a delta - this updates the snapshot and applies the delta to all delta trackers
	AddClusterDeltas([]*pkgresource.DeltaClusterSnapshot)
}

type FullsnapshotManager struct {
	snapshot resource.ClusterSnapshot
	lock     sync.RWMutex
	children []*deltaTrackerFull
}

func NewFullSnapshotManager() FullSnapshotManager {
	return &FullsnapshotManager{
		snapshot: resource.ClusterSnapshot{},
	}
}

func (s *FullsnapshotManager) ShallowCopySnapshots(selectors ...resource.GVKSelectorFunc) resource.ClusterSnapshot {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.snapshot.ShallowCopy(selectors...)
}

func (s *FullsnapshotManager) TrackDeltas() pkgresource.DeltaTracker {
	d := &deltaTrackerFull{
		parent: s,
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	s.children = append(s.children, d)

	return d
}

func (s *FullsnapshotManager) AddClusterDeltas(deltaSnapshots []*pkgresource.DeltaClusterSnapshot) {
	s.lock.Lock()
	defer s.lock.Unlock()
	for _, d := range deltaSnapshots {
		d.ApplyToSnapshot(s.snapshot)
		for _, child := range s.children {
			child.addDelta(d)
		}
	}
}

func (s *FullsnapshotManager) Len() int {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return len(s.snapshot)
}

func (s *FullsnapshotManager) Read(f func(snap resource.ClusterSnapshot)) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	f(s.snapshot)
}

func (s *FullsnapshotManager) Write(f func(snap resource.ClusterSnapshot) error) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	err := f(s.snapshot)
	// we don't know what's changed, so clear out all deltas
	for _, child := range s.children {
		// clear out deltas
		child.clearDelta()
	}
	return err
}

type deltaTrackerFull struct {
	parent        *FullsnapshotManager
	deltaLock     sync.Mutex
	deltaSnapshot *pkgresource.DeltaClusterSnapshot
}

func (d *deltaTrackerFull) DeltaSnapshot(selectors ...resource.GVKSelectorFunc) (resource.ClusterSnapshot, *pkgresource.DeltaClusterSnapshot) {
	d.parent.lock.RLock()
	defer d.parent.lock.RUnlock()
	d.deltaLock.Lock()
	defer d.deltaLock.Unlock()

	snap := d.deltaSnapshot
	if snap == nil {
		snap = pkgresource.NewDeltaClusterSnapshot()
	} else {
		// add empty delta for each cluster that doesn't have one:
		snapshots := snap.Snapshots()
		for cluster := range d.parent.snapshot {
			if _, ok := snapshots[cluster]; !ok {
				snapshots[cluster] = pkgresource.NewDeltaSnapshot()
			}
		}
	}

	d.deltaSnapshot = pkgresource.NewDeltaClusterSnapshot()
	return d.parent.snapshot.ShallowCopy(selectors...), snap
}

func (d *deltaTrackerFull) addDelta(dcs *pkgresource.DeltaClusterSnapshot) {
	d.deltaLock.Lock()
	defer d.deltaLock.Unlock()
	// the first time delta snapshot is nil, as we have no delta...
	if d.deltaSnapshot != nil {
		d.deltaSnapshot.Merge(dcs)
	}
}

func (d *deltaTrackerFull) clearDelta() {
	d.deltaLock.Lock()
	defer d.deltaLock.Unlock()
	d.deltaSnapshot = nil
}
