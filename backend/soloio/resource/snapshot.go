package resource

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/solo-io/skv2/pkg/controllerutils"
	"github.com/solo-io/skv2/pkg/ezkube"
	"github.com/solo-io/skv2/pkg/resource"
)

type DeltaSnapshot struct {
	updates resource.Snapshot
	removed map[schema.GroupVersionKind]map[types.NamespacedName]struct{}
}

func NewDeltaSnapshot() *DeltaSnapshot {
	return &DeltaSnapshot{
		updates: make(map[schema.GroupVersionKind]map[types.NamespacedName]resource.TypedObject),
		removed: make(map[schema.GroupVersionKind]map[types.NamespacedName]struct{}),
	}
}

func MakeDeltaFromSnapshots(old, new resource.Snapshot) *DeltaSnapshot {
	if old == nil {
		old = resource.Snapshot{}
	}
	if new == nil {
		new = resource.Snapshot{}
	}
	delta := NewDeltaSnapshot()
	for gvk, objects := range new {
		oldObjects, oldHadKind := old[gvk]
		if !oldHadKind {
			delta.updates[gvk] = objects
			continue
		}

		for ref, obj := range objects {

			var objUnchanged bool
			if oldObj, ok := oldObjects[ref]; ok {
				if controllerutils.ObjectsEqual(obj, oldObj) &&
					controllerutils.ObjectStatusesEqual(obj, oldObj) {
					objUnchanged = true
				}
			}

			if objUnchanged {
				continue
			}
			// if objects not equal, this is an update
			delta.updates.Insert(gvk, obj)
		}
	}
	for gvk, oldObjects := range old {
		newObjects, hasKind := new[gvk]
		if !hasKind {
			delta.removed[gvk] = map[types.NamespacedName]struct{}{}
			for ref := range oldObjects {
				// each obj for this gvk was removed
				delta.removed[gvk][ref] = struct{}{}
			}
			continue
		}

		for oldRef := range oldObjects {

			objDeleted := true
			if _, ok := newObjects[oldRef]; ok {
				objDeleted = false
			}

			if !objDeleted {
				continue
			}

			// if objects not equal, this is an update
			removed, ok := delta.removed[gvk]
			if !ok {
				removed = map[types.NamespacedName]struct{}{}
			}
			removed[oldRef] = struct{}{}
			delta.removed[gvk] = removed
		}
	}
	return delta
}

// indicates whether the delta contains any changes
func (delta *DeltaSnapshot) String() string {
	builder := strings.Builder{}
	for gvk, objs := range delta.GetUpdated() {
		builder.WriteString(fmt.Sprintf("{%v_total_updated: %d}", gvk, len(objs)))
	}
	for gvk, objs := range delta.GetRemoved() {
		builder.WriteString(fmt.Sprintf("{%v_total_removed: %d}", gvk, len(objs)))
	}
	return builder.String()
}

// indicates whether the delta contains any changes
func (delta *DeltaSnapshot) Empty() bool {
	return delta == nil || (len(delta.updates) == 0 && len(delta.removed) == 0)
}

// indicates whether the delta contains any changes
func (delta *DeltaSnapshot) Clone() *DeltaSnapshot {
	// Empty and nil are equivalent, so this is safe
	if delta == nil {
		return nil
	}
	result := map[schema.GroupVersionKind]map[types.NamespacedName]struct{}{}
	for gvk, objects := range delta.removed {
		result[gvk] = map[types.NamespacedName]struct{}{}
		for key := range objects {
			result[gvk][key] = struct{}{}
		}
	}
	return &DeltaSnapshot{
		updates: delta.updates.Clone(),
		removed: result,
	}
}

// apply the delta to the snapshot
func (delta *DeltaSnapshot) ApplyToSnapshot(cluster string, snapshot resource.Snapshot) {
	for gvk, ids := range delta.removed {
		for id := range ids {
			snapshot.Delete(gvk, id)
		}
	}
	for gvk, resources := range delta.updates {
		for _, resource := range resources {
			ezkube.SetClusterName(resource, cluster)
			snapshot.Insert(gvk, resource)
		}
	}
}

// merge another delta into this one
func (delta *DeltaSnapshot) Merge(patch *DeltaSnapshot) {
	for gvk, nnsSet := range patch.removed {
		for nns := range nnsSet {
			delta.SetRemoved(gvk, nns)
		}
	}
	for gvk, resources := range patch.updates {
		for _, resource := range resources {
			delta.SetUpdated(gvk, resource)
		}
	}
}

// FilterByGVK destructively removes anything in the delta that doesn't match the given GVK selector.
// This method performs map deletions and is not thread-safe
func (delta *DeltaSnapshot) FilterByGVK(selector resource.GVKSelectorFunc) {
	if delta == nil {
		return
	}

	for gvk := range delta.removed {
		if !selector(gvk) {
			delete(delta.removed, gvk)
		}
	}

	for gvk := range delta.updates {
		if !selector(gvk) {
			delete(delta.updates, gvk)
		}
	}
}

// add an updated obj to the delta
func (delta *DeltaSnapshot) SetUpdated(gvk schema.GroupVersionKind, obj resource.TypedObject) {
	ns := obj.GetNamespace()
	n := obj.GetName()
	nns := types.NamespacedName{Namespace: ns, Name: n}

	if nnsSet, ok := delta.removed[gvk]; ok {
		delete(nnsSet, nns)
	}

	if _, ok := delta.updates[gvk]; !ok {
		delta.updates[gvk] = make(map[types.NamespacedName]resource.TypedObject)
	}
	delta.updates[gvk][nns] = obj
}

// add an removed obj to the delta
func (delta *DeltaSnapshot) GetUpdated() map[schema.GroupVersionKind]map[types.NamespacedName]resource.TypedObject {
	if delta == nil {
		return nil
	}
	return delta.updates
}

// add an removed obj to the delta
func (delta *DeltaSnapshot) SetRemoved(gvk schema.GroupVersionKind, nns types.NamespacedName) {
	if nnsSet, ok := delta.updates[gvk]; ok {
		delete(nnsSet, nns)
	}
	if _, ok := delta.removed[gvk]; !ok {
		delta.removed[gvk] = make(map[types.NamespacedName]struct{})
	}
	delta.removed[gvk][nns] = struct{}{}
}

// add an removed obj to the delta
func (delta *DeltaSnapshot) GetRemoved() map[schema.GroupVersionKind]map[types.NamespacedName]struct{} {
	if delta == nil {
		return nil
	}
	return delta.removed
}

type DeltaClusterSnapshot struct {
	snapshotMap map[string]*DeltaSnapshot
}

func NewDeltaClusterSnapshot() *DeltaClusterSnapshot {
	return &DeltaClusterSnapshot{
		snapshotMap: map[string]*DeltaSnapshot{},
	}
}

func MakeDeltaFromClusterSnapshots(old, new resource.ClusterSnapshot) *DeltaClusterSnapshot {
	delta := NewDeltaClusterSnapshot()
	// Iterate through all old snapshots and make deltas
	for cluster, v := range old {
		delta.snapshotMap[cluster] = MakeDeltaFromSnapshots(v, new[cluster])
	}

	// Also iterate through all new snaps, and do a diff with the missing values
	for cluster, v := range new {
		if _, ok := delta.snapshotMap[cluster]; !ok {
			delta.snapshotMap[cluster] = MakeDeltaFromSnapshots(old[cluster], v)
		}
	}

	return delta
}

// ApplyToSnapshot may modify the passed in snap, so either copy before, or lock
func (d *DeltaClusterSnapshot) ApplyToSnapshot(snap resource.ClusterSnapshot) {
	for cluster, delta := range d.snapshotMap {
		clusterSnap, ok := snap[cluster]
		if !ok {
			// No data from this cluster
			snap[cluster] = map[schema.GroupVersionKind]map[types.NamespacedName]resource.TypedObject{}
			clusterSnap = snap[cluster]
		}
		delta.ApplyToSnapshot(cluster, clusterSnap)
	}
}

func (d *DeltaClusterSnapshot) Snapshots() map[string]*DeltaSnapshot {
	return d.snapshotMap
}

func (d *DeltaClusterSnapshot) Merge(patch *DeltaClusterSnapshot) {
	for cluster, delta := range patch.snapshotMap {
		d.MergeDelta(cluster, delta)
	}
}

func (d *DeltaClusterSnapshot) MergeDelta(cluster string, patch *DeltaSnapshot) {
	if patch == nil {
		// we lost the data from this cluster
		d.snapshotMap[cluster] = nil
		return
	}
	clusterSnap, ok := d.snapshotMap[cluster]
	if !ok {
		// No data from this cluster
		clusterSnap = NewDeltaSnapshot()
		d.snapshotMap[cluster] = clusterSnap
	}
	clusterSnap.Merge(patch)
}

// FilterByGVK destructively removes anything in any cluster's delta that doesn't match the given GVK selector
func (d *DeltaClusterSnapshot) FilterByGVK(selector resource.GVKSelectorFunc) {
	if d == nil {
		return
	}

	for _, delta := range d.snapshotMap {
		delta.FilterByGVK(selector)
	}
}

func (d *DeltaClusterSnapshot) loadOrCreateSnap(cluster string) *DeltaSnapshot {
	clusterSnap, ok := d.snapshotMap[cluster]
	if !ok {
		d.snapshotMap[cluster] = NewDeltaSnapshot()
		clusterSnap = d.snapshotMap[cluster]
	}
	return clusterSnap
}

func (d *DeltaClusterSnapshot) SetUpdated(cluster string, gvk schema.GroupVersionKind, obj resource.TypedObject) {
	clusterSnap := d.loadOrCreateSnap(cluster)
	clusterSnap.SetUpdated(gvk, obj)
}

func (d *DeltaClusterSnapshot) SetRemoved(cluster string, gvk schema.GroupVersionKind, nns types.NamespacedName) {
	clusterSnap := d.loadOrCreateSnap(cluster)
	clusterSnap.SetRemoved(gvk, nns)
}

func (d *DeltaClusterSnapshot) Empty() bool {
	if d == nil {
		return true
	}
	for _, diff := range d.snapshotMap {
		if !diff.Empty() {
			return false
		}
	}
	return true
}

// Get a summary of the delta's contents for debugging
func (d *DeltaClusterSnapshot) Summary() string {
	if d == nil {
		return ""
	}
	updated := map[string]map[schema.GroupVersionKind]uint64{}
	removed := map[string]map[schema.GroupVersionKind]uint64{}
	for cluster, delta := range d.Snapshots() {
		updated[cluster] = map[schema.GroupVersionKind]uint64{}
		removed[cluster] = map[schema.GroupVersionKind]uint64{}
		for gvk, resources := range delta.GetRemoved() {
			removed[cluster][gvk] += uint64(len(resources))
		}
		for gvk, resources := range delta.GetUpdated() {
			updated[cluster][gvk] += uint64(len(resources))
		}
	}

	return fmt.Sprintf("{updated:%v, removed:%v}", updated, removed)
}
