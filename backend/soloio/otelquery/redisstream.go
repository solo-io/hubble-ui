package otelquery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/solo-io/gloo-otelcollector-contrib/exporter/redisstreamexporter"
	"github.com/solo-io/go-utils/contextutils"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/cilium/hubble-ui/backend/soloio/syncutils"
)

type RedisStreamConfig struct {
	// Number of logs that should be returned. Incompatible with `since/until`.
	// Defaults to the most recent (last) `number` logs, unless `first` is
	// true, then it will return the earliest `number` logs.
	Number int64
	// first specifies if we should look at the first `number` logs or the
	// last `number` of logs. Incompatible with `follow`.
	First bool
	// follow sets when the server should continue to stream logs after
	// printing the last N logs.
	Follow bool
	// Since this time for returned logs. Incompatible with `number`.
	Since *timestamppb.Timestamp
	// Until this time for returned logs. Incompatible with `number`.
	Until *timestamppb.Timestamp
	// Cluster to filter logs by. If empty, all logs will be returned.
	Cluster string
	// Only query for log data with these attributes
	LogAttributes []AttributeFilter
	// Only query for log data for resources with these attributes
	ResourceAttributes []AttributeFilter
}

func (q *RedisStreamConfig) validate() error {
	if q.First && q.Follow {
		return fmt.Errorf("cannot specify both first and follow")
	}

	return nil
}

// TODO(tim): what's the difference between this constant and the one in
// the defaults package?
const ClusterNameKey = "cluster_name"

type AttributeFilter struct {
	Name  string
	Value interface{}
}

func (a AttributeFilter) GetName() string {
	return a.Name
}

func (a AttributeFilter) GetValue() pcommon.Value {
	v := pcommon.NewValueEmpty()
	err := v.FromRaw(a.Value)
	if err != nil {
		return pcommon.NewValueEmpty()
	}

	return v
}

func StreamRedisWithClient(
	ctx context.Context,
	client redis.UniversalClient,
	req *RedisStreamConfig,
) (<-chan plog.ResourceLogs, error) {
	return streamRedis(ctx, client, req)
}

func StreamRedis(
	ctx context.Context,
	endpoint string,
	req *RedisStreamConfig,
) (<-chan plog.ResourceLogs, error) {
	client := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:       []string{endpoint},
		ReadTimeout: time.Second * 10,
	})
	return streamRedis(ctx, client, req)
}

func streamRedis(
	ctx context.Context,
	client redis.UniversalClient,
	req *RedisStreamConfig,
) (<-chan plog.ResourceLogs, error) {
	err := req.validate()
	if err != nil {
		return nil, err
	}

	queryEngine := NewRedisOtelLogsQueryEngine(
		ctx,
		client,
		redisstreamexporter.DefaultStream,
	)

	if req.Follow {
		cahn, err := queryEngine.Follow(ctx, req)
		if err != nil {
			return nil, err
		}
		return cahn, nil
	}

	canh := make(chan plog.ResourceLogs)
	go func() {
		defer close(canh)
		rls, err := queryEngine.Fetch(req)
		if err != nil {
			contextutils.LoggerFrom(ctx).Errorw("error fetching logs", zap.Error(err))
			return
		}
		for i := 0; i < rls.Len(); i++ {
			rl := rls.At(i)
			canh <- rl
		}
	}()
	return canh, nil
}

type RedisOtelLogsQueryEngine struct {
	logChan chan plog.ResourceLogs
	// endpoint    string
	stream      string
	redisClient redis.UniversalClient

	topicManager       redisstreamexporter.TopicReadClient
	streamManagers     syncutils.AtomicMap[uint64, *redisStreamManager]
	streamMgrWaitGroup sync.WaitGroup
	streamMgrContext   context.Context
	streamMgrCancel    context.CancelFunc
}

func NewRedisOtelLogsQueryEngine(
	ctx context.Context,
	client redis.UniversalClient,
	stream string,
) *RedisOtelLogsQueryEngine {
	topicManager := redisstreamexporter.NewTopicReadClient(
		&redisstreamexporter.Config{
			Stream: stream,
		},
		client,
		redisstreamexporter.WithLogger(contextutils.LoggerFrom(ctx).Desugar()),
		redisstreamexporter.WithPullInterval(5*time.Second),
	)

	streamMgrContext, streamMgrCancel := context.WithCancel(ctx)
	return &RedisOtelLogsQueryEngine{
		// endpoint:         endpoint,
		stream:           stream,
		redisClient:      client,
		logChan:          make(chan plog.ResourceLogs),
		streamManagers:   syncutils.AtomicMap[uint64, *redisStreamManager]{},
		streamMgrContext: streamMgrContext,
		streamMgrCancel:  streamMgrCancel,
		topicManager:     topicManager,
	}
}

func (r *RedisOtelLogsQueryEngine) Fetch(
	query *RedisStreamConfig,
) (plog.ResourceLogsSlice, error) {
	result := plog.NewResourceLogsSlice()

	// make sure the streams that are known by the stream meta tracker are current
	err := r.topicManager.PullTopics(context.Background())
	if err != nil {
		return result, err
	}

	err = r.getAndStartStreamManagers(query, false)
	if err != nil {
		return result, err
	}

	go func() {
		r.streamMgrWaitGroup.Wait()
		close(r.logChan)
	}()

	count := 0
	for log := range r.logChan {
		log.MoveTo(result.AppendEmpty())

		count++ // increments count
		if query.Number > 0 && count >= int(query.Number) {
			break
		}
	}

	return result, nil
}

func (r *RedisOtelLogsQueryEngine) Follow(
	ctx context.Context,
	query *RedisStreamConfig,
) (<-chan plog.ResourceLogs, error) {
	result := make(chan plog.ResourceLogs)

	r.topicManager.StartPullSyncLoop(ctx)

	streamManagersShutdown := make(chan struct{})

	// periodically check if new streams are created that match the query
	go func() {
		ticker := time.NewTicker(time.Second * 5)
		for {
			select {
			case <-r.streamMgrContext.Done():
				r.streamMgrWaitGroup.Wait()
				streamManagersShutdown <- struct{}{}
				close(streamManagersShutdown)
				return
			case <-ticker.C:
				err := r.getAndStartStreamManagers(query, true)
				if err != nil {
					contextutils.LoggerFrom(ctx).Errorf(
						"Error getting stream managers: %v",
						err,
					)
				}
			}
		}
	}()

	// send results down the stream
	go func() {
		count := 0
	outer:
		for {
			select {
			case <-ctx.Done():
				break outer
			case log := <-r.logChan:
				result <- log
				count++ // increments count
				if query.Number != 0 && count >= int(query.Number) {
					break outer
				}
			}
		}

		r.streamMgrCancel()
		<-streamManagersShutdown
		close(r.logChan)
		close(result)
	}()

	return result, nil
}

func (r *RedisOtelLogsQueryEngine) getAndStartStreamManagers(
	query *RedisStreamConfig,
	follow bool,
) error {
	logAttrFilt := []redisstreamexporter.TopicAttributeFilter{}
	for _, filter := range query.LogAttributes {
		logAttrFilt = append(logAttrFilt, filter)
	}

	resourceAttrFilt := []redisstreamexporter.TopicAttributeFilter{}
	for _, filter := range query.ResourceAttributes {
		resourceAttrFilt = append(resourceAttrFilt, filter)
	}

	topics, err := r.topicManager.GetTopicsForAttributes(
		logAttrFilt,
		resourceAttrFilt,
	)
	if err != nil {
		return err
	}

	for _, stream := range topics {
		if _, ok := r.streamManagers.Get(stream.TopicID); !ok {
			err := r.spawnStreamManager(stream.TopicID, query, follow)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *RedisOtelLogsQueryEngine) spawnStreamManager(
	topicID uint64,
	query *RedisStreamConfig,
	follow bool,
) error {
	streamManager := newRedisManager(
		redisstreamexporter.TopicWithIdKey(r.stream, topicID),
		query.Since,
		query.Until,
		r.redisClient,
	)

	r.streamManagers.Set(topicID, streamManager)
	r.streamMgrWaitGroup.Add(1)

	go func() {
		defer r.streamMgrWaitGroup.Done()
		ticker := time.NewTicker(50 * time.Millisecond)
		for {
			select {
			case <-r.streamMgrContext.Done():
				return
			case <-ticker.C:
				logs, err := streamManager.query(r.streamMgrContext, query)
				if err != nil {
					contextutils.LoggerFrom(r.streamMgrContext).Errorf(
						"There was an error fetching the logs for stream %v: %v",
						topicID,
						err,
					)
				}
				for _, log := range logs {
					for i := 0; i < log.ResourceLogs().Len(); i++ {
						r.logChan <- log.ResourceLogs().At(i)
					}
				}
				if !follow {
					return
				}
			}
		}
	}()
	return nil
}

type redisStreamManager struct {
	rdb            redis.UniversalClient
	stream         string
	logUnmarshaler plog.ProtoUnmarshaler

	lastID string
	until  string
}

func newRedisManager(
	stream string,
	since, until *timestamppb.Timestamp,
	rdb redis.UniversalClient,
) *redisStreamManager {
	rm := &redisStreamManager{
		rdb:    rdb,
		stream: stream,
		// initialize lastID as everything
		lastID: "-",
		until:  "+",
	}

	if since != nil {
		rm.lastID = fmt.Sprintf("%v", since.AsTime().UnixMilli())
	}

	if until != nil {
		rm.until = fmt.Sprintf("%v", until.AsTime().UnixMilli())
	}

	return rm
}

func (rm *redisStreamManager) query(
	ctx context.Context,
	req *RedisStreamConfig,
) ([]plog.Logs, error) {
	var results []redis.XMessage
	var err error

	number := req.Number
	if req.First {
		results, err = rm.rdb.XRangeN(ctx, rm.stream, rm.lastID, rm.until, number).Result()
	} else {
		results, err = rm.rdb.XRevRangeN(ctx, rm.stream, rm.until, rm.lastID, number).Result()
		// RevRange will return oldest first, which we need to correct for.
		swapOrder(results)
	}
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, nil
	}

	if rm.lastID == results[len(results)-1].ID {
		return nil, nil
	}

	var logsArr []plog.Logs
	for _, result := range results {
		rm.lastID = result.ID

		logsVal, ok := result.Values[redisstreamexporter.LogsKey]
		if !ok {
			contextutils.LoggerFrom(ctx).Debugf("logs key not found in stream '%v' at key '%v'", rm.stream, redisstreamexporter.LogsKey)
			continue
		}

		logsValStr, ok := logsVal.(string)
		if !ok {
			contextutils.LoggerFrom(ctx).Errorf("logs value is not expected type 'string'. Is %T", logsVal)
			continue
		}

		logs, err := rm.logUnmarshaler.UnmarshalLogs([]byte(logsValStr))
		if err != nil {
			contextutils.LoggerFrom(ctx).Errorf("could not unmarshal logs from stream: %v", err)
			continue
		}

		logsArr = append(logsArr, logs)
	}

	return logsArr, nil
}

func swapOrder[T any](arr []T) {
	for i := 0; i < len(arr)/2; i++ {
		arr[i], arr[len(arr)-1-i] = arr[len(arr)-1-i], arr[i]
	}
}
