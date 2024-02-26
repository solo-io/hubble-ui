package remote

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/avast/retry-go"
	"github.com/redis/go-redis/v9"
	"github.com/rotisserie/eris"
	"github.com/solo-io/go-utils/contextutils"
	v1 "github.com/solo-io/skv2/pkg/api/core.skv2.solo.io/v1"
	"github.com/solo-io/skv2/pkg/ezkube"
	"github.com/solo-io/skv2/pkg/resource"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	relay_resource "github.com/cilium/hubble-ui/backend/soloio/relay/resource"
	"github.com/cilium/hubble-ui/backend/soloio/relay/v1alpha1"
	pkgresource "github.com/cilium/hubble-ui/backend/soloio/resource"
)

const (
	redisStreamBlockingTime = time.Second * 30
	redisStreamFloorLen     = 64
	redisKeySeparator       = "~" // Not "." so it doesn't interfere with kube delimiter
	RedisKeySet             = "gloo.mesh.key.set"
	RedisStream             = "gloo.mesh.diff.stream"
	redisStreamKey          = "stream.diff.key"

	RedisWriteTimeMetricName = "redis_write_time_sec"
	RedisSyncErrMetricName   = "redis_sync_err"

	RedisReadTimeMetricName = "redis_read_time_sec"
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

func (o *Options) applyDefaults() {
	if o.RedisWatchRateLimit == 0 {
		o.RedisWatchRateLimit = time.Second / 4
	}
}

func NewRedisPersistenceClient(
	ctx context.Context,
	client redis.UniversalClient,
	scheme *runtime.Scheme,
	opts Options,
) (*redisPersistenceClient, error) {
	// Give connecting a few tries
	if err := retry.Do(
		func() error {
			if _, err := client.Ping(ctx).Result(); err != nil {
				return err
			}
			return nil
		},
	); err != nil {
		return nil, err
	}
	b := &bytes.Buffer{}
	// Key buffer is equal to max length of a redis key
	// 64 characters for cluster name, 4 (rune) for separator.
	b.Grow((64 * 2) + 4)
	opts.applyDefaults()
	rdc := &redisPersistenceClient{
		client:      client,
		scheme:      scheme,
		commonCache: newCommonCache(),
		keyBuilder:  b,
		options:     opts,
	}

	// readAll once on startup
	streamId, err := rdc.readAll(ctx)
	if err != nil {
		return nil, err
	}

	rdc.signalHandlers(ezkube.ConvertRefToId(RedisReadEvent()))
	rdc.streamId = streamId

	go func() {
		ticker := time.NewTicker(opts.RedisWatchRateLimit)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// If context has not finished, we should continue long polling
				if err := rdc.handleDiff(ctx); err != nil {
					contextutils.LoggerFrom(ctx).Errorw(
						"Error reconciling redis state",
						zap.Error(err),
					)
					// redisSyncError.Inc() // FIXME: add metric
				}
			}
		}
	}()

	return rdc, nil
}

// This function will read all data from redis.
// It is acceptable for this function to return "", nil
// This state means that the stream is empty, and we cannot figure out the last ID,
// it should only happen when a pod reads before any data has been written.
func (r *redisPersistenceClient) readAll(ctx context.Context) (string, error) {
	allHmaps, err := r.client.SMembers(ctx, RedisKeySet).Result()
	if err != nil {
		return "", err
	}

	// Setup a transaction pipeline to get all data at once
	tx := r.client.TxPipeline()
	for _, hmap := range allHmaps {
		tx.HGetAll(ctx, hmap)
	}

	// Get the stream info for the diff stream
	tx.XRevRangeN(ctx, RedisStream, "+", "-", 1)
	results, err := tx.Exec(ctx)
	if err != nil {
		return "", err
	}

	var streamId string
	err = r.snapshots.Write(func(snapshot resource.ClusterSnapshot) error {
		for _, result := range results {
			switch typedResult := result.(type) {
			case *redis.XMessageSliceCmd:
				// This is the result for the XRevRangeN CMD which gets the last item
				if len(typedResult.Val()) == 0 {
					continue
				}
				// We set the returned to 1, so we read the first val
				streamId = typedResult.Val()[0].ID
			case *redis.MapStringStringCmd:
				// This is the result for the HGetAll CMD which gets all items in a given hash map
				if len(typedResult.Args()) != 2 {
					continue
				}
				if typedResult.Args()[0].(string) != "hgetall" {
					continue
				}
				hmapKey := typedResult.Args()[1].(string)
				clusterName, gvk := r.parseRedisKey(hmapKey)
				clusterSnapshot, ok := snapshot[clusterName]
				if !ok {
					snapshot[clusterName] = map[schema.GroupVersionKind]map[types.NamespacedName]resource.TypedObject{}
					clusterSnapshot = snapshot[clusterName]
				}
				gvkSnap, err := buildGvkMapFromRaw(ctx, gvk, r.scheme, typedResult.Val())
				if err != nil {
					return err
				}
				clusterSnapshot[gvk] = gvkSnap
			default:
				// skipping unknown typed result
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	return streamId, nil
}

func (r *redisPersistenceClient) parseRedisKey(key string) (string, schema.GroupVersionKind) {
	pieces := strings.Split(key, redisKeySeparator)
	return pieces[0], schema.GroupVersionKind{
		Group:   pieces[1],
		Version: pieces[2],
		Kind:    pieces[3],
	}
}

func (r *redisPersistenceClient) handleDiff(ctx context.Context) error {
	if r.streamId == "" {
		newStreamId, err := r.readAll(ctx)
		if err != nil {
			return err
		}
		// Store the new stream ID
		// Signal that we have read all the data from redis
		r.signalHandlers(ezkube.ConvertRefToId(RedisReadEvent()))
		r.streamId = newStreamId
		return nil
	}
	result, err := r.client.XRead(
		ctx, &redis.XReadArgs{
			Streams: []string{RedisStream, r.streamId},
			Block:   redisStreamBlockingTime,
		},
	).Result()
	if err != nil {
		// Nil is the error returned when there is no new data after blocking
		if errors.Is(err, redis.Nil) {
			return nil
		}
		return err
	}
	if len(result) != 1 {
		contextutils.LoggerFrom(ctx).DPanicf("Our stream is missing from the list %+v", result)
		// Reset the streamId so that we readAll on the next iteration
		r.streamId = ""
		return nil
	}

	// Thanks Josh!
	// If our key is not in the list, redis will return everything, so we need to readAll.
	// Set streamID to "" and return
	if len(result[0].Messages) >= r.redisStreamLen() {
		// Reset the streamId so that we readAll on the next iteration
		contextutils.LoggerFrom(ctx).
			Warnf("The stream returned more values than the length of the stream, need to reset and get state of the world from redis because the stream list has grown too large.")
		r.streamId = ""
		return nil
	}

	// Create a slice of deltas to apply after they are unmarshalled
	deltaSnapshots := make([]*pkgresource.DeltaClusterSnapshot, 0, len(result[0].Messages))
	for _, msg := range result[0].Messages {
		deltaSnapshot, err := BuildSnapshotFromMessage(msg, r.scheme)
		if err != nil {
			contextutils.LoggerFrom(ctx).DPanicw("error building delta snapshot, this should never happen", zap.Error(err))
			r.streamId = ""
			return err
		}
		// Gather all of hte snapshots to apply atomically.
		deltaSnapshots = append(deltaSnapshots, deltaSnapshot)
	}

	// Must lock AFTER long polling so that lock doesn't get held
	// APPLY SNAPSHOT
	r.snapshots.AddClusterDeltas(deltaSnapshots)
	// END APPLY SNAPSHOT

	// Signal that the latest diffs have been applied
	r.signalHandlers(ezkube.ConvertRefToId(RedisReadEvent()))

	numMessages := len(result[0].Messages)
	// Set the streamId to the latest message Id received
	if numMessages != 0 {
		contextutils.LoggerFrom(ctx).Debugf("Setting streamId after read (%s)", result[0].Messages[0].ID)
		r.streamId = result[0].Messages[numMessages-1].ID
	} else {
		contextutils.LoggerFrom(ctx).Debugf("Skipping streamId update as no messages were received.")
	}
	return nil
}

func (r *redisPersistenceClient) redisStreamLen() int {
	streamLen := r.options.RedisStreamMaxLen
	if streamLen == 0 {
		// If the stream length is not overwritten, we want to use the number of clusters as the scale factor
		streamLen = 32 * r.snapshots.Len()
		// set a floor length for the stream if not explicitly overwritten
		if streamLen < redisStreamFloorLen {
			streamLen = redisStreamFloorLen
		}
	}
	return streamLen
}

func RedisReadEvent() *v1.ClusterObjectRef {
	return &v1.ClusterObjectRef{
		Name:        "redis-read-event",
		Namespace:   "",
		ClusterName: "",
	}
}

// BuildSnapshotFromMessage builds the delta snapshot from a given redis stream message.
func BuildSnapshotFromMessage(
	msg redis.XMessage,
	scheme *runtime.Scheme,
) (*pkgresource.DeltaClusterSnapshot, error) {
	diff, ok := msg.Values[redisStreamKey]
	if !ok {
		return nil, eris.Errorf("A stream message is missing our stream key, %+v", msg.Values)
	}
	bytDiff, ok := diff.(string)
	if !ok {
		return nil, eris.Errorf("Our stream message key contained a non string entry, %+v", bytDiff)
	}
	protoDiff := v1alpha1.ClusterResourcePatch{}
	if err := proto.Unmarshal([]byte(bytDiff), &protoDiff); err != nil {
		return nil, err
	}
	deltaSnapshot, err := relay_resource.ConvertClusterDelta(scheme, &protoDiff)
	if err != nil {
		return nil, err
	}
	return deltaSnapshot, nil
}
