package redisstreamexporter

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
)

const LogsKey = "logs"

type redisExporter struct {
	config *Config
	logger *zap.Logger

	rdb           redis.UniversalClient
	logsMarshaler *plog.ProtoMarshaler
	lastExpireSet *time.Time

	topicManager *TopicManager
}

func initExporter(cfg *Config, createSettings exporter.CreateSettings) *redisExporter {
	logger := createSettings.Logger

	redisClient := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{cfg.Endpoint},
	})

	// these attributes are used to create the stream name so we want to make sure
	// they're sorted so the same set of attributes will always create the same
	// stream name no matter which order they're defined in.
	sort.Strings(cfg.TopicAttributes.ResourceAttributes)
	sort.Strings(cfg.TopicAttributes.LogAttributes)

	return &redisExporter{
		config:        cfg,
		logger:        logger,
		rdb:           redisClient,
		logsMarshaler: &plog.ProtoMarshaler{},
		topicManager:  NewTopicManager(cfg, redisClient, WithLogger(logger)),
	}
}

func newLogsExporter(
	ctx context.Context,
	params exporter.CreateSettings,
	cfg *Config,
) (exporter.Logs, error) {
	e := initExporter(cfg, params)

	topicMgrCtx, topicMgrCancel := context.WithCancel(ctx)
	return exporterhelper.NewLogsExporter(
		ctx,
		params,
		cfg,
		e.pushLogsData,
		exporterhelper.WithTimeout(cfg.TimeoutSettings),
		exporterhelper.WithRetry(cfg.RetrySettings),
		exporterhelper.WithQueue(cfg.QueueSettings),
		exporterhelper.WithStart(func(context.Context, component.Host) error {
			e.topicManager.StartSyncLoop(topicMgrCtx)
			return nil
		}),
		exporterhelper.WithShutdown(func(context.Context) error {
			topicMgrCancel()
			return nil
		}),
	)
}

func (re *redisExporter) pushLogsData(ctx context.Context, ld plog.Logs) error {
	// bucketLogsByTopic mutates the logs, so we need to copy the logs here
	// otherwise other exporters in this pipeline will get a mutated record
	copy := plog.NewLogs()
	ld.CopyTo(copy)

	// This also upserts the topic metadata into redis
	logsByTopic, err := bucketLogsByTopic(re.config, copy, re.topicManager)

	if err != nil {
		return err
	}
	for topicID, logs := range logsByTopic {
		streamName := TopicWithIdKey(re.config.Stream, topicID)

		b, err := re.logsMarshaler.MarshalLogs(logs)
		if err != nil {
			return fmt.Errorf("could not marshal logs: %w", err)
		}

		err = re.sendRedis(ctx, streamName, b)
		if err != nil {
			return fmt.Errorf("could not send logs to redis: %w", err)
		}

		if re.shouldExpire() {
			// We could set expire outside of the logs push, but if logs are not being sent via redis then it seems natural
			// that we would want them to expire after a certain interval. Redis streams are meant for inspecting real-time
			// logging data, not for long-term storage.
			err := re.expireRedis(ctx, streamName)
			if err != nil {
				return fmt.Errorf("could not set expire for logs in redis: %w", err)
			}
		}
	}

	re.logger.Info("Connected successfully, exporting logs....")
	return nil
}

func (re *redisExporter) sendRedis(
	ctx context.Context,
	streamName string,
	b []byte,
) error {
	_, err := re.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamName,
		MaxLen: re.config.MaxEntries,
		Values: []interface{}{
			LogsKey,
			b,
		},
		ID: "*",
		// 	Much better performance.
		Approx: true,
	}).Result()

	return err
}

func (re *redisExporter) shouldExpire() bool {
	if re.lastExpireSet == nil {
		return true
	}

	// Return true if it has been half of the expire time since we've last renewed.
	if time.Since(*re.lastExpireSet) > re.config.Expire/2 {
		return true
	}

	return false
}

func (re *redisExporter) expireRedis(
	ctx context.Context,
	streamName string,
) error {
	_, err := re.rdb.Expire(ctx, streamName, re.config.Expire).Result()
	if err != nil {
		return err
	}

	if re.lastExpireSet == nil {
		re.lastExpireSet = new(time.Time)
	}

	*re.lastExpireSet = time.Now()

	return nil
}
