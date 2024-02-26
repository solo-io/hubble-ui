package resource

import (
	"encoding/json"
	"fmt"

	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/rotisserie/eris"
	"github.com/solo-io/skv2/pkg/ezkube"
	skresource "github.com/solo-io/skv2/pkg/resource"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cilium/hubble-ui/backend/soloio/relay/v1alpha1"
	pkgresource "github.com/cilium/hubble-ui/backend/soloio/resource"
)

func ConvertDelta(
	objCreater runtime.ObjectCreater,
	protoDelta *v1alpha1.ResourcePatch,
) (*pkgresource.DeltaSnapshot, error) {
	delta := pkgresource.NewDeltaSnapshot()
	for _, rr := range protoDelta.Resources {
		ri := rr.GetId()
		gvk := schema.GroupVersionKind{
			Group:   ri.Gvk.Group,
			Version: ri.Gvk.Version,
			Kind:    ri.Gvk.Kind,
		}

		runtimeObj, err := objCreater.New(gvk)
		if err != nil {
			return nil, err
		}
		obj, ok := runtimeObj.(skresource.TypedObject)
		if !ok {
			return nil, eris.Errorf("internal error: cannot convert %T to TypedObject", runtimeObj)
		}
		if err := UnmarshalObjectAny(rr.Resource, obj); err != nil {
			return nil, err
		}
		delta.SetUpdated(gvk, obj)
	}

	for _, rr := range protoDelta.RemovedResources {
		gvk := schema.GroupVersionKind{
			Group:   rr.Gvk.Group,
			Version: rr.Gvk.Version,
			Kind:    rr.Gvk.Kind,
		}
		nns := types.NamespacedName{
			Name:      rr.Name,
			Namespace: rr.Namespace,
		}

		delta.SetRemoved(gvk, nns)
	}

	return delta, nil
}

func ConvertClusterDelta(
	objCreater runtime.ObjectCreater,
	protoDelta *v1alpha1.ClusterResourcePatch,
) (*pkgresource.DeltaClusterSnapshot, error) {
	clusterPatch := pkgresource.NewDeltaClusterSnapshot()
	for cluster, patch := range protoDelta.GetPatches() {
		if delta, err := ConvertDelta(objCreater, patch); err != nil {
			return nil, err
		} else {
			clusterPatch.Snapshots()[cluster] = delta
		}
	}
	return clusterPatch, nil
}

func CreateClusterPatch(clusterDelta *pkgresource.DeltaClusterSnapshot) *v1alpha1.ClusterResourcePatch {
	if clusterDelta == nil {
		return nil
	}
	clusterPatch := &v1alpha1.ClusterResourcePatch{
		Patches: map[string]*v1alpha1.ResourcePatch{},
	}
	for cluster, delta := range clusterDelta.Snapshots() {
		clusterPatch.Patches[cluster] = createPatch(delta, nil)
	}
	return clusterPatch
}

func createPatch(
	delta *pkgresource.DeltaSnapshot,
	statusUpdateGvks []schema.GroupVersionKind,
) *v1alpha1.ResourcePatch {
	var removed []*v1alpha1.ResourceIdentifier
	for gvk, nnss := range delta.GetRemoved() {
		for nns := range nnss {
			removed = append(removed, convertResourceId(gvk, nns))
		}
	}

	var updated []*v1alpha1.Resource
	for gvk, nnss := range delta.GetUpdated() {
		for nns, obj := range nnss {
			updated = append(updated, convertResource(gvk, nns, obj))
		}
	}

	statusUpdateGvkProtos := ConvertProtoGvk(statusUpdateGvks)

	return &v1alpha1.ResourcePatch{
		Resources:        updated,
		RemovedResources: removed,
		StatusUpdateGvks: statusUpdateGvkProtos,
	}
}

func CreateDeltaResponse(
	delta *pkgresource.DeltaSnapshot,
	nonce int,
	statusUpdateGvks []schema.GroupVersionKind,
) *v1alpha1.RelayDeltaResponse {
	return &v1alpha1.RelayDeltaResponse{
		SystemVersionInfo: "",
		ResourcePatch:     createPatch(delta, statusUpdateGvks),
		Nonce:             fmt.Sprintf("%v", nonce),
		More:              false,
	}
}

func convertResourceId(
	gvk schema.GroupVersionKind,
	nns types.NamespacedName,
) *v1alpha1.ResourceIdentifier {
	return &v1alpha1.ResourceIdentifier{
		Name:      nns.Name,
		Namespace: nns.Namespace,
		Gvk: &v1alpha1.GVK{
			Group:   gvk.Group,
			Version: gvk.Version,
			Kind:    gvk.Kind,
		},
	}
}

func convertResource(
	gvk schema.GroupVersionKind,
	nns types.NamespacedName,
	obj skresource.TypedObject,
) *v1alpha1.Resource {
	return &v1alpha1.Resource{
		Id:       convertResourceId(gvk, nns),
		Resource: objToAny(gvk, obj),
	}
}

func objToAny(gvk schema.GroupVersionKind, obj skresource.TypedObject) *any.Any {
	// remove server-set metadata fields
	objToMarshal := obj.DeepCopyObject().(skresource.TypedObject)
	zeroServerMetadataFields(objToMarshal)

	// set gvk in typemeta; required
	objToMarshal.GetObjectKind().SetGroupVersionKind(gvk)

	// convert obj to json
	data, err := json.Marshal(objToMarshal)
	// err should never happen
	if err != nil {
		// TODO: maybe panic maybe not..
		panic(err)
	}

	var bv wrappers.BytesValue

	bv.Value = data

	a, err := anypb.New(&bv)
	if err != nil {
		// TODO: maybe panic maybe not..
		panic(err)
	}

	return a
}

// zero out fields that are set by the server before we push
// also zeros out ClusterName which may be set by translators before push/pull
// we want to preserve resource version for status updates.
// TODO(ilackarms): consider running this on the side that receives the object
func zeroServerMetadataFields(obj client.Object) {
	obj.SetDeletionTimestamp(nil)
	obj.SetDeletionGracePeriodSeconds(nil)
	// This use to set to empty, but now that we have the annotation deleting it makes more sense.
	delete(obj.GetAnnotations(), ezkube.ClusterAnnotation)
}

func ConvertGvk(protogvks []*v1alpha1.GVK) []schema.GroupVersionKind {
	var gvks []schema.GroupVersionKind
	for _, protogvk := range protogvks {
		gvks = append(
			gvks, schema.GroupVersionKind{
				Group:   protogvk.Group,
				Version: protogvk.Version,
				Kind:    protogvk.Kind,
			},
		)
	}

	return gvks
}

func ConvertProtoGvk(gvks []schema.GroupVersionKind) []*v1alpha1.GVK {
	var protogvks []*v1alpha1.GVK
	for _, gvk := range gvks {
		protogvks = append(
			protogvks, &v1alpha1.GVK{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind,
			},
		)
	}

	return protogvks
}

func UnmarshalObjectAny(objAny *any.Any, obj client.Object) error {
	var bv wrappers.BytesValue
	err := anypb.UnmarshalTo(objAny, &bv, proto.UnmarshalOptions{})
	if err != nil {
		return err
	}

	// convert obj to json
	return json.Unmarshal(bv.Value, obj)
}
