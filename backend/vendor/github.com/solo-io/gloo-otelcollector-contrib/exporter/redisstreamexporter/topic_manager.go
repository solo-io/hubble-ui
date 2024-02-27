package redisstreamexporter

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// TopicManager is used to manage metadata for the topics we're exporting to.
// This metadata is stored in redis alongside the streams and can be used for
// debugging/inspecting the topics/streams and the attributes that created them.
// The list of topics is organized into a redis set of keys and the metadata
// is stored serialized in those keys.

type TopicManager struct {
	config      *Config
	redisClient redis.UniversalClient

	topics    map[uint64]*topicMeta // topicID -> topicMeta
	topicsMtx sync.Mutex

	keyTTL       time.Duration
	pullInterval time.Duration
	syncInterval time.Duration
	logger       *zap.Logger
}

type topicMeta struct {
	TopicID            uint64
	LogAttributes      []TopicAttr
	ResourceAttributes []TopicAttr
	LastSeen           *atomic.Time
}

type TopicManagerOption func(*TopicManager)

func NewTopicManager(
	config *Config,
	redisClient redis.UniversalClient,
	options ...TopicManagerOption,
) *TopicManager {
	tm := &TopicManager{
		config:       config,
		redisClient:  redisClient,
		topics:       map[uint64]*topicMeta{},
		keyTTL:       12 * time.Hour,
		pullInterval: 10 * time.Second,
		syncInterval: 25 * time.Second,
		logger:       zap.NewNop(),
	}
	for _, option := range options {
		option(tm)
	}
	return tm
}

func WithKeyTTL(ttl time.Duration) TopicManagerOption {
	return func(tm *TopicManager) {
		tm.keyTTL = ttl
	}
}

func WithPullInterval(interval time.Duration) TopicManagerOption {
	return func(tm *TopicManager) {
		tm.pullInterval = interval
	}
}

func WithSyncInterval(interval time.Duration) TopicManagerOption {
	return func(tm *TopicManager) {
		tm.syncInterval = interval
	}
}

func WithLogger(logger *zap.Logger) TopicManagerOption {
	return func(tm *TopicManager) {
		tm.logger = logger
	}
}

// TopicReadClient is the interface that is used to interact w/ with topics
// in a Read-only way
type TopicReadClient interface {
	GetTopicsForAttributes(logAttributes, resourceAttributes []TopicAttributeFilter) ([]*topicMeta, error)
	PullTopics(ctx context.Context) error
	StartPullSyncLoop(ctx context.Context)
}

func NewTopicReadClient(
	config *Config,
	redisClient redis.UniversalClient,
	options ...TopicManagerOption,
) TopicReadClient {
	tm := NewTopicManager(config, redisClient, options...)
	return tm
}

// PutTopicMeta is used to make this instance topic manager aware of a topic and
// its metadata. This metadata will be flushed to redis if it is not already known.
func (s *TopicManager) PutTopicMeta(
	topicID uint64,
	logAttributes []TopicAttr,
	resoucreAttributes []TopicAttr,
) error {
	if _, ok := s.topics[topicID]; !ok {
		s.topicsMtx.Lock()
		defer s.topicsMtx.Unlock()
		if _, ok := s.topics[topicID]; !ok {
			s.logger.Debug("putting topic metadata", zap.Uint64("topic_id", topicID))
			topic := topicMeta{
				TopicID:            topicID,
				LogAttributes:      logAttributes,
				ResourceAttributes: resoucreAttributes,
				LastSeen:           atomic.NewTime(time.Now()),
			}
			s.topics[topicID] = &topic
			err := s.flushTopicMeta(context.Background(), &topic)
			if err != nil {
				return err
			}
		}

	}
	s.topics[topicID].LastSeen.Store(time.Now())
	return nil
}

func (s *TopicManager) flushTopicMeta(ctx context.Context, topic *topicMeta) error {
	s.logger.Debug("flushing topic metadata for topic", zap.Uint64("topic_id", topic.TopicID))

	_, err := s.redisClient.SAdd(
		ctx,
		TopicMetaKey(s.config.Stream),
		topic.TopicID,
	).Result()
	if err != nil {
		return err
	}

	results, err := topic.marshal()
	if err != nil {
		return err
	}

	_, err = s.redisClient.Set(
		ctx,
		TopicWithIdMetaKey(s.config.Stream, topic.TopicID),
		results,
		s.keyTTL,
	).Result()
	if err != nil {
		return err
	}

	return nil
}

// GetTopics returns a list of all topics that this instance is aware of.
func (s *TopicManager) GetTopics() []topicMeta {
	s.topicsMtx.Lock()
	defer s.topicsMtx.Unlock()
	topics := make([]topicMeta, len(s.topics))
	i := 0
	for _, topic := range s.topics {
		topics[i] = *topic
		i++
	}
	return topics
}

type TopicAttributeFilter interface {
	GetName() string
	GetValue() pcommon.Value
}

// GetTopicsForAttributes returns a list of topic IDs that match the given
// attributes. The attributes are ANDed together, so a topic must match all
// attributes to be returned.
//
// TODO - this is a pretty naive implementation, we should probably use a
// different data structure to index the topics by attribute to make more
// efficient.
func (s *TopicManager) GetTopicsForAttributes(
	logAttributes []TopicAttributeFilter,
	resourceAttributes []TopicAttributeFilter,
) ([]*topicMeta, error) {
	results := []*topicMeta{}
outer:
	for _, topic := range s.topics {
		for _, attr := range logAttributes {
			attrFound := false
			for _, topicAttr := range topic.LogAttributes {
				if topicAttr.Name == attr.GetName() {
					attrFound = true
					attrHash, err := hashAttrValue(attr.GetValue())
					if err != nil {
						return nil, err
					}
					topicAttrHash, err := hashAttrValue(topicAttr.Value)
					if err != nil {
						return nil, err
					}
					if attrHash != topicAttrHash {
						continue outer
					}
				}
			}
			if !attrFound {
				continue outer
			}
		}

		for _, attr := range resourceAttributes {
			attrFound := false
			for _, topicAttr := range topic.ResourceAttributes {
				if topicAttr.Name == attr.GetName() {
					attrFound = true
					attrHash, err := hashAttrValue(attr.GetValue())
					if err != nil {
						return nil, err
					}
					topicAttrHash, err := hashAttrValue(topicAttr.Value)
					if err != nil {
						return nil, err
					}
					if attrHash != topicAttrHash {
						continue outer
					}
				}
			}
			if !attrFound {
				continue outer
			}
		}
		results = append(results, topic)
	}

	return results, nil
}

// PullTopics is used to pull all topics from redis and make this instance
// synchronized w/ persisted state.
func (s *TopicManager) PullTopics(ctx context.Context) error {
	s.logger.Debug("pulling topics")

	topicIDs, err := s.redisClient.SMembers(
		ctx,
		TopicMetaKey(s.config.Stream),
	).Result()
	if err != nil {
		return err
	}

	for _, ss := range topicIDs {
		s.logger.Debug("pulling topic", zap.String("topic_id", ss))

		topicID, err := strconv.ParseUint(ss, 10, 64)
		if err != nil {
			return err
		}
		ser, err := s.redisClient.Get(
			ctx,
			TopicWithIdMetaKey(s.config.Stream, topicID),
		).Result()

		if err != nil {
			// this can happen -- it's possible the topic was expired and GC hasn't
			// updated the set yet.
			s.logger.Debug(
				"skipping pulling topic that was not found in redis",
				zap.Uint64("topic_id", topicID),
				zap.Error(err),
			)
			continue
		}

		meta := &topicMeta{}
		err = meta.unmarshal([]byte(ser))
		if err != nil {
			return err
		}

		// always use the latest timestamp ...
		s.topicsMtx.Lock()
		if currTopic, ok := s.topics[meta.TopicID]; ok {
			if currTopic.LastSeen.Load().After(meta.LastSeen.Load()) {
				meta.LastSeen = currTopic.LastSeen
				err = s.flushTopicMeta(ctx, meta) // write back the latest timestamp
				if err != nil {
					s.logger.Warn(fmt.Sprintf("failed to update topic metadata timestamp %d", meta.TopicID), zap.Error(err))
				}
			}
		}
		s.topics[meta.TopicID] = meta
		s.topicsMtx.Unlock()
	}

	return nil
}

// SyncTopics pulls then list of topics from redis in case of other producers,
// it then runs GC on the topic metadata. This is just updating TTLs
// on the keys that shouldn't be GC'd (which should have TTLs), so this is like
// the 'mark' phase. The 'sweep' phase is handled by redis through key's TTL.
// It also removes keys from set that contains the list of topic IDs.
func (s *TopicManager) SyncTopics(ctx context.Context) error {
	s.logger.Debug("running topic metadata sync & GC")

	if err := s.PullTopics(ctx); err != nil {
		s.logger.Warn("failed to pull topics before GC", zap.Error(err))
		return err
	}

	for _, topic := range s.GetTopics() {
		ttl := time.Duration(s.keyTTL.Milliseconds() - (time.Now().UnixMilli() - topic.LastSeen.Load().UnixMilli()))
		keyExists, err := s.redisClient.Expire(
			ctx,
			TopicWithIdMetaKey(s.config.Stream, topic.TopicID),
			ttl*time.Millisecond,
		).Result()
		if err != nil {
			s.logger.Warn("GC Error updating topic metadata TTL",
				zap.Uint64("topic_id", topic.TopicID),
				zap.Error(err),
			)
			return err
		}

		// it's possible the key might already be expired, so this will keep the set
		// of topic IDs in sync w/ the actual topics
		if !keyExists {
			s.logger.Debug("removing topic from set", zap.Uint64("topic_id", topic.TopicID))
			s.topicsMtx.Lock()
			delete(s.topics, topic.TopicID)
			s.topicsMtx.Unlock()
		}
	}

	// keep updated the members of the set that contains the list of topic IDs
	topicIDs, err := s.redisClient.SMembers(
		ctx,
		TopicMetaKey(s.config.Stream),
	).Result()
	if err != nil {
		s.logger.Warn("GC was not able to fetch topic IDs", zap.Error(err))
		return err
	}
	for _, t := range topicIDs {
		topicID, err := strconv.ParseUint(t, 10, 64)
		if err != nil {
			s.logger.Warn("GC was not able to parse topic ID -- deleting", zap.Error(err))
		}

		s.topicsMtx.Lock()
		_, ok := s.topics[uint64(topicID)]
		s.topicsMtx.Unlock()
		if !ok || err != nil {
			s.logger.Debug("removing topic from metadata set", zap.Uint64("topic_id", uint64(topicID)))
			_, err := s.redisClient.SRem(
				ctx,
				TopicMetaKey(s.config.Stream),
				t,
			).Result()
			if err != nil {
				s.logger.Warn("GC was not able to remove topic ID from set", zap.Error(err))
			}
		}
	}

	// any topic IDs we know about that aren't in the meta set should actually be
	// added. This could theoretically happen in two cases:
	// 1 - someome deleted the topic meta set from redis
	// 2 - another instance of the exporter didn't know about the topic (b/c
	//		 weirdly it's not receiving any logs for that topic and it hasn't yet
	//		 run it's pull loop) so the other instance and GC'd it.
	for _, topic := range s.GetTopics() {
		found := false
		for _, t := range topicIDs {
			tasuint, _ := strconv.ParseUint(t, 10, 64)
			if tasuint == topic.TopicID {
				found = true
				break
			}
		}

		if !found {
			_, err := s.redisClient.SAdd(
				ctx,
				TopicMetaKey(s.config.Stream),
				topic.TopicID,
			).Result()
			if err != nil {
				s.logger.Warn(fmt.Sprintf("unable to re-add topic ID %d to list of topics", topic.TopicID), zap.Error(err))
			}
		}
	}

	return nil
}

// StartPullSyncLoop keeps the topic metadata in memory in sync w/ what is
// stored in redis by periodically pulling the list of topics from redis.
// This is useful for processes that wish to consume the topics
func (s *TopicManager) StartPullSyncLoop(ctx context.Context) {
	s.logger.Debug("starting pull sync loop with interval", zap.Duration("interval", s.pullInterval))

	go func() {
		ticker := time.NewTicker(s.pullInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.PullTopics(ctx); err != nil {
					s.logger.Warn("failed to pull topics", zap.Error(err))
				}
			}
		}
	}()
}

func (s *TopicManager) StartSyncLoop(ctx context.Context) {
	s.logger.Debug("starting topic metadata sync loop with interval", zap.Duration("interval", s.syncInterval))

	go func() {
		ticker := time.NewTicker(s.syncInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.SyncTopics(ctx); err != nil {
					s.logger.Warn("error syncing topic metadata", zap.Error(err))
				}
			}
		}
	}()
}

func (s *topicMeta) marshal() ([]byte, error) {
	serVal := struct {
		TopicID       uint64
		LastSeen      time.Time
		LogAttributes []struct {
			Name  string
			Value interface{}
		}
		ResourceAttributes []struct {
			Name  string
			Value interface{}
		}
	}{
		TopicID:  s.TopicID,
		LastSeen: s.LastSeen.Load(),
	}

	for _, attr := range s.LogAttributes {
		serVal.LogAttributes = append(
			serVal.LogAttributes,
			struct {
				Name  string
				Value interface{}
			}{
				Name:  attr.Name,
				Value: attr.Value.AsRaw(),
			},
		)
	}

	for _, attr := range s.ResourceAttributes {
		serVal.ResourceAttributes = append(
			serVal.ResourceAttributes,
			struct {
				Name  string
				Value interface{}
			}{
				Name:  attr.Name,
				Value: attr.Value.AsRaw(),
			},
		)
	}

	ser, err := yaml.Marshal(serVal)
	if err != nil {
		return nil, err
	}
	return ser, nil
}

func (s *topicMeta) unmarshal(data []byte) error {
	serVal := struct {
		TopicID       uint64
		LastSeen      time.Time
		LogAttributes []struct {
			Name  string
			Value interface{}
		}
		ResourceAttributes []struct {
			Name  string
			Value interface{}
		}
	}{}

	err := yaml.Unmarshal(data, &serVal)
	if err != nil {
		return err
	}

	s.TopicID = serVal.TopicID
	s.LastSeen = atomic.NewTime(serVal.LastSeen)

	for _, attr := range serVal.LogAttributes {
		val := pcommon.NewValueEmpty()
		err := val.FromRaw(attr.Value)
		if err != nil {
			return err
		}
		s.LogAttributes = append(
			s.LogAttributes,
			TopicAttr{
				Name:  attr.Name,
				Value: val,
			},
		)
	}

	for _, attr := range serVal.ResourceAttributes {
		val := pcommon.NewValueEmpty()
		err := val.FromRaw(attr.Value)
		if err != nil {
			return err
		}
		s.ResourceAttributes = append(
			s.ResourceAttributes,
			TopicAttr{
				Name:  attr.Name,
				Value: val,
			},
		)
	}

	return nil
}
