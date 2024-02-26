package resource

import (
	"github.com/solo-io/skv2/pkg/resource"
)

type SnapshotManager interface {
	// get current snapshot
	ShallowCopySnapshots(selectors ...resource.GVKSelectorFunc) resource.ClusterSnapshot
	// get a delta tracker to track deltas
	TrackDeltas() DeltaTracker
}

type DeltaTracker interface {
	// Get the the current snapshot and a delta from a previous call to this function.
	// This function may return a nil delta snapshot for a cluster (and will always do so the first time it is called)
	// this means that the delta is unknown, and the caller should assume that the entire snapshot has changed.
	// Note that selectors is ignored for the delta snapshot, to avoid memory leaks.
	DeltaSnapshot(selectors ...resource.GVKSelectorFunc) (resource.ClusterSnapshot, *DeltaClusterSnapshot)
}
