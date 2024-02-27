package flow_stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	pbFlow "github.com/cilium/cilium/api/v1/flow"
	"github.com/cilium/cilium/api/v1/observer"
	hubblev1 "github.com/cilium/cilium/pkg/hubble/api/v1"
	"github.com/cilium/cilium/pkg/hubble/filters"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/solo-io/gloo-otelcollector-contrib/receiver/hubblereceiver/common"
	"github.com/solo-io/go-utils/contextutils"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/cilium/hubble-ui/backend/domain/labels"
	"github.com/cilium/hubble-ui/backend/domain/service"
	"github.com/cilium/hubble-ui/backend/internal/msg"
	grpc_errors "github.com/cilium/hubble-ui/backend/pkg/grpc_utils/errors"
	"github.com/cilium/hubble-ui/backend/soloio/otelquery"
)

const (
	// Ref(GME): pkg/defaults/defaults.go
	redisStreamResourceAttributeSourceKey = "source"

	flowStreamReconnectInterval = 2 * time.Second
)

type soloFlowStream struct {
	log         logrus.FieldLogger
	redisClient redis.UniversalClient

	flowStreamLogs <-chan plog.ResourceLogs
	req            *observer.GetFlowsRequest

	flows    chan *pbFlow.Flow
	errors   chan error
	stop     chan struct{}
	stopOnce sync.Once

	redisConnected bool
	manager        *flowManager
}

func NewSolo(
	log logrus.FieldLogger,
	redisClient redis.UniversalClient,
) (*soloFlowStream, error) {
	return &soloFlowStream{
		log:         log,
		redisClient: redisClient,
		stop:        make(chan struct{}),
		stopOnce:    sync.Once{},
		manager:     newFlowManager(),
	}, nil
}

func (h *soloFlowStream) runLoop(
	ctx context.Context,
	iterFn func(<-chan plog.ResourceLogs) error,
) {
	isEOFFatal := false
	tryEOFReconnect := false
	numTransientErrors := 0
	isDatumFetched := false

F:
	for {
		if h.shouldStop(ctx) {
			break
		}

		isRenewed, err := h.ensureConnection(ctx)
		if err != nil {
			h.log.WithError(err).Error("ensureConnection failed")
			h.sendError(ctx, err)
			return
		}

		if isRenewed {
			isEOFFatal = false
			tryEOFReconnect = false
			numTransientErrors = 0
			isDatumFetched = false

			h.log.Debug("connection is renewed")
		}

		if h.flowStreamLogs == nil {
			h.log.Debug("recreating flow stream")
			err := h.recreateFlowStream(ctx)
			if err != nil {
				h.log.
					WithError(err).
					// WithField("connection-addr", fmt.Sprintf("%p", h.connection)).
					Warn("recreateFlowStream failed")

				h.log.Debug("error, will try again in %s", flowStreamReconnectInterval)
				time.Sleep(flowStreamReconnectInterval)
				continue F
			}

			h.log.Debug("recreateFlowStream finished")
		}

		select {
		case <-ctx.Done():
			return
		case <-h.stop:
			return
		default:
			err := iterFn(h.flowStreamLogs)

			// NOTE: Once we get proper response, we reset all error flags and counters
			if err == nil {
				isEOFFatal = false
				tryEOFReconnect = false
				numTransientErrors = 0
				isDatumFetched = true
				continue F
			}

			if errors.Is(err, context.Canceled) || grpc_errors.IsCancelled(err) {
				break F
			}

			log := h.log.WithError(err).
				WithField("MaxNumOfEOF", MaxNumOfEOF).
				WithField("nEOFS", numTransientErrors)

			if errors.Is(err, io.EOF) {
				if isDatumFetched {
					log.Info("EOF occurred after datum is fetched. Stream ends.")
					return
				}

				numTransientErrors += 1
				tryEOFReconnect = tryEOFReconnect || numTransientErrors == NumEOFForReconnect
				isEOFFatal = isEOFFatal || numTransientErrors >= MaxNumOfEOF
				log = log.
					WithField("EOFReconnect", tryEOFReconnect).
					WithField("MaxEOFReached", isEOFFatal)

				switch {
				case isEOFFatal:
					log.Warn(
						"EOF occurred after reconnect. Timescape has no data.",
					)

					return
				case tryEOFReconnect:
					tryEOFReconnect = false
					log.Warn("EOF occurred several times, trying reconnect")

					if err := h.reconnect(ctx); err != nil {
						h.sendError(ctx, err)
						return
					}

					continue F
				default:
					continue F
				}
			} else {
				log.Warn("fetching Flow from underlying flow stream failed")
			}

			h.sendError(ctx, err)
			break F
		}
	}
}

func (h *soloFlowStream) Run(ctx context.Context, req *observer.GetFlowsRequest) {
	h.req = req
	h.log.
		WithField("FlowStream", fmt.Sprintf("%p", h)).
		Debug("running")

	allowList, denyList, err := buildFilters(ctx, req)
	if err != nil {
		h.sendError(ctx, err)
		goto done
	}

	h.runLoop(ctx, func(flowStreamLogs <-chan plog.ResourceLogs) error {
		flowLog, ok := <-flowStreamLogs

		// logChan will be closed when there are no more logs to stream. This won't happen if the user wants to
		// follow the logs.
		if !ok {
			// in case the flowStream`` was just closed, nil it
			h.flowStreamLogs = nil
			return nil
		}

		for _, flowResponse := range h.manager.logToFlowResponses(ctx, flowLog) {
			f := flowResponse.GetFlow()
			if f == nil {
				continue
			}

			e := hubblev1.Event{
				Timestamp: flowResponse.Time,
				Event:     f,
			}
			// filter evt!
			if !filters.Apply(allowList, denyList, &e) {
				continue
			}

			h.handleFlow(ctx, f)
		}
		return nil
	})

done:
	h.Stop()
}

func (h *soloFlowStream) Stop() {
	h.stopOnce.Do(func() {
		if h.stop != nil {
			close(h.stop)
		}
	})

	h.log.
		WithField("FlowStream", fmt.Sprintf("%p", h)).
		Info("FlowStream has been stopped")
}

func (h *soloFlowStream) Stopped() chan struct{} {
	return h.stop
}

func (h *soloFlowStream) Errors() chan error {
	if h.errors == nil {
		h.errors = make(chan error)
	}

	return h.errors
}

func (h *soloFlowStream) Flows() chan *pbFlow.Flow {
	if h.flows == nil {
		h.flows = make(chan *pbFlow.Flow)
	}

	return h.flows
}

func (h *soloFlowStream) CollectLimit(
	ctx context.Context,
	req *observer.GetFlowsRequest,
	limit int64,
) ([]*pbFlow.Flow, error) {
	go h.Run(ctx, req)
	defer h.Stop()

	flows := make([]*pbFlow.Flow, 0)
	for {
		select {
		case <-ctx.Done():
			return flows, ctx.Err()
		case err := <-h.Errors():
			return nil, err
		case f := <-h.Flows():
			if f == nil {
				continue
			}

			if limit != -1 && int64(len(flows)) >= limit {
				return flows, nil
			}

			flows = append(flows, f)
		case <-h.Stopped():
			h.log.
				WithField("nflows", len(flows)).
				Debug("CollectLimit is finished")

			return flows, ctx.Err()
		}
	}
}

func (h *soloFlowStream) handleFlow(ctx context.Context, f *pbFlow.Flow) {
	if f.GetL4() == nil || f.GetSource() == nil || f.GetDestination() == nil {
		return
	}
	sourceId, destId := service.IdsFromFlowProto(f)
	if sourceId == "0" || destId == "0" {
		h.log.Warnf(msg.ZeroIdentityInSourceOrDest)
		h.printZeroIdentityFlow(f)
		return
	}

	// TODO: workaround to hide flows/services which are showing as "World",
	// but actually they are k8s services without initialized pods.
	// Appropriate fix is to construct and show special service map cards
	// and show these flows in special way inside flows table.
	if f.GetDestination() != nil {
		destService := f.GetDestinationService()
		destLabelsProps := labels.Props(f.GetDestination().GetLabels())
		destNames := f.GetDestinationNames()
		isDestOutside := destLabelsProps.IsWorld || len(destNames) > 0

		if destService != nil && isDestOutside {
			return
		}
	}

	h.sendFlow(ctx, f)
}

func (h *soloFlowStream) sendFlow(ctx context.Context, f *pbFlow.Flow) {
	select {
	case <-ctx.Done():
	case <-h.stop:
	case h.Flows() <- f:
	}
}

func (h *soloFlowStream) sendError(ctx context.Context, err error) {
	select {
	case <-ctx.Done():
	case <-h.stop:
	case h.errors <- err:
	}
}

func (h *soloFlowStream) shouldStop(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		if h.stop == nil {
			return true
		}

		select {
		case <-ctx.Done():
			return true
		case <-h.stop:
			return true
		default:
		}
	}

	return false
}

func (h *soloFlowStream) printZeroIdentityFlow(f *pbFlow.Flow) {
	serialized, err := json.Marshal(f)
	if err != nil {
		h.log.Errorf("failed to marshal flow to json: %v\n", err)
		return
	}

	h.log.WithField("json", string(serialized)).Warn("zero identity flow")
}

func (h *soloFlowStream) ensureConnection(_ context.Context) (bool, error) {
	if h.redisConnected {
		return false, nil
	}

	// FIXME: HACK
	h.redisConnected = true
	h.flowStreamLogs = nil
	return true, nil
}

func (h *soloFlowStream) reconnect(_ context.Context) error {
	h.flowStreamLogs = nil

	// FIXME: HACK
	return nil
}

func (h *soloFlowStream) recreateFlowStream(ctx context.Context) error {
	flowsRequest := getQueryConfig("")
	logChan, err := otelquery.StreamRedisWithClient(ctx, h.redisClient, flowsRequest)
	if err != nil {
		return fmt.Errorf("could not stream redis %w", err)
	}

	h.flowStreamLogs = logChan
	return nil
}

// looks silly but makes writing loops easier
func arr[T any](a ...T) []T { return a }

func getQueryConfig(cluster string) *otelquery.RedisStreamConfig {
	number := ^uint32(0)

	req := &otelquery.RedisStreamConfig{
		Number: int64(number), // TODO: should this be zero?
		Follow: true,
		Since:  nil, // TODO: should this be timestamppb.Now() instead of nil?
		Until:  nil,
		First:  false,
		ResourceAttributes: []otelquery.AttributeFilter{
			{
				Name:  redisStreamResourceAttributeSourceKey,
				Value: "hubble",
			},
		},
	}

	if cluster != "" {
		req.ResourceAttributes = []otelquery.AttributeFilter{
			{
				Name:  "cluster_name",
				Value: cluster,
			},
		}
	}

	return req
}

type flowManager struct {
	decoder *common.FlowDecoder
}

func newFlowManager() *flowManager {
	// TODO: we eventually may want to make this a configurable decoding
	decoder, err := common.NewFlowDecoder(common.EncodingJSONBASE64)
	if err != nil {
		// Should never happen
		panic(err)
	}

	return &flowManager{
		decoder: decoder,
	}
}

func (fm *flowManager) logToFlowResponses(ctx context.Context, flowLog plog.ResourceLogs) []*observer.GetFlowsResponse {
	var responses []*observer.GetFlowsResponse
	resource := flowLog.Resource()
	for i := 0; i < flowLog.ScopeLogs().Len(); i++ {
		scopeLog := flowLog.ScopeLogs().At(i)
		for j := 0; j < scopeLog.LogRecords().Len(); j++ {
			lr := scopeLog.LogRecords().At(j)

			val := lr.Body()
			if val.Type() != pcommon.ValueTypeStr {
				contextutils.LoggerFrom(ctx).Errorf("log record not of expected byte type: %v", val)
				continue
			}

			ciliumFlow, err := fm.decoder.FromValue(val.Str())
			if err != nil {
				contextutils.LoggerFrom(ctx).Errorf("could not decode flow: %v", err)
				continue
			}

			nodeName, ok := resource.Attributes().Get(common.OTelAttrK8sNodeName)
			if !ok {
				contextutils.LoggerFrom(ctx).Errorf("node name missing from log record: %v", lr)
			}
			nodeNameStr := nodeName.AsString()
			// fix the node name to have the cluster name
			clusterName, ok := resource.Attributes().Get("cluster_name")
			if ok {
				split := strings.SplitN(nodeNameStr, "/", 2)
				if len(split) == 2 {
					nodeNameStr = fmt.Sprintf("%s/%s", clusterName.AsString(), split[1])
				} else { // len is 1 or 0.
					nodeNameStr = fmt.Sprintf("%s/%s", clusterName.AsString(), nodeNameStr)
				}

				// very sad: https://github.com/cilium/hubble-ui/blob/4d7ad932f18bbe6b1d93f03fd13fb5c3c3ce5dcb/src/domain/labels.ts#L202
				// if no existing cluster label, add one
				const clusterLabel1 = "k8s:io.cilium.k8s.policy.cluster"
				const clusterLabel2 = "io.cilium.k8s.policy.cluster"
			EpLoop:
				for _, ep := range arr(ciliumFlow.Source, ciliumFlow.Destination) {
					if ep == nil {
						continue
					}
					for _, label := range ep.Labels {
						spl := strings.SplitN(label, "=", 2)
						if len(spl) != 2 {
							continue
						}
						key, _ := spl[0], spl[1]
						if key == clusterLabel1 || key == clusterLabel2 {
							continue EpLoop
						}
					}
					ep.Labels = append(ep.Labels, fmt.Sprintf("%s=%s", clusterLabel1, clusterName.AsString()))
				}

				ciliumFlow.NodeName = nodeNameStr

				responses = append(responses, &observer.GetFlowsResponse{
					Time:     timestamppb.New(lr.Timestamp().AsTime()),
					NodeName: nodeNameStr,
					ResponseTypes: &observer.GetFlowsResponse_Flow{
						Flow: ciliumFlow,
					},
				})
			}
		}
	}

	return responses
}

func buildFilters(ctx context.Context, req *observer.GetFlowsRequest) (filters.FilterFuncs, filters.FilterFuncs, error) {
	var allowList []*pbFlow.FlowFilter
	var denyList []*pbFlow.FlowFilter
	for _, f := range req.Whitelist {
		if f == nil {
			continue
		}
		allowList = append(allowList, f)
	}
	for _, f := range req.Blacklist {
		if f == nil {
			continue
		}
		denyList = append(denyList, f)
	}

	filterList := filters.DefaultFilters
	allowListFuncs, err := filters.BuildFilterList(ctx, allowList, filterList)
	if err != nil {
		return nil, nil, err
	}
	denyListFuncs, err := filters.BuildFilterList(ctx, denyList, filterList)
	if err != nil {
		return nil, nil, err
	}
	return allowListFuncs, denyListFuncs, nil
}
