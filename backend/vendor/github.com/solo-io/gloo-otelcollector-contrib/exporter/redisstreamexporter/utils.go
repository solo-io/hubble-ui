package redisstreamexporter

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

// bucketLogsByTopic returns a map of topic IDs to logs. This will modify the
// logs argument so if you're using the log record for something useful after
// calling this, it would be wise to copy it first.
func bucketLogsByTopic(
	config *Config,
	logs plog.Logs,
	topicManager *TopicManager,
) (map[uint64]plog.Logs, error) {
	resourceLogsByTopic := []map[uint64]plog.ResourceLogs{}

	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		resourceLogs := logs.ResourceLogs().At(i)

		rl, err := bucketResourceLogsByTopic(config, resourceLogs, topicManager)
		if err != nil {
			return nil, err
		}
		resourceLogsByTopic = append(resourceLogsByTopic, rl)
	}

	result := toLogsByTopic(
		logs,
		toResourceLogsSliceByTopic(resourceLogsByTopic...),
	)
	return result, nil
}

// bucketResourceLogsByTopic returns a map of topic IDs to logs.
func bucketResourceLogsByTopic(
	config *Config,
	resourceLogs plog.ResourceLogs,
	topicManager *TopicManager,
) (map[uint64]plog.ResourceLogs, error) {
	scopeLogsByTopic := []map[uint64]plog.ScopeLogs{}
	for i := 0; i < resourceLogs.ScopeLogs().Len(); i++ {
		scopeLog := resourceLogs.ScopeLogs().At(i)
		logsByTopic, err := bucketScopedLogsByTopic(
			config,
			resourceLogs,
			scopeLog.LogRecords(),
			topicManager,
		)
		if err != nil {
			return nil, err
		}

		scopeLogsByTopic = append(
			scopeLogsByTopic,
			toScopeLogsByTopic(scopeLog, logsByTopic),
		)
	}

	result := toResourceLogsByTopic(
		resourceLogs,
		toScopeLogsSliceByTopic(scopeLogsByTopic...),
	)
	return result, nil
}

type TopicAttr struct {
	Name  string
	Value pcommon.Value
}

func (s TopicAttr) GetName() string {
	return s.Name
}

func (s TopicAttr) GetValue() pcommon.Value {
	return s.Value
}

func bucketScopedLogsByTopic(
	config *Config,
	resourceLog plog.ResourceLogs,
	logRecords plog.LogRecordSlice,
	topicManager *TopicManager,
) (map[uint64]plog.LogRecordSlice, error) {
	logsByTopic := map[uint64]plog.LogRecordSlice{}
	resourceAttrs := make(
		[]TopicAttr,
		len(config.TopicAttributes.ResourceAttributes),
	)
	logAttrs := make([]TopicAttr, len(config.TopicAttributes.LogAttributes))

	for i := 0; i < logRecords.Len(); i++ {
		logRecord := logRecords.At(i)

		for j, resourceAttrName := range config.TopicAttributes.ResourceAttributes {
			val, ok := resourceLog.Resource().Attributes().Get(resourceAttrName)
			if ok {
				resourceAttrs[j] = TopicAttr{
					Name:  resourceAttrName,
					Value: val,
				}
			} else {
				resourceAttrs[j] = TopicAttr{
					Name:  resourceAttrName,
					Value: pcommon.NewValueEmpty(),
				}
			}
		}

		for j, logAttrName := range config.TopicAttributes.LogAttributes {
			val, ok := logRecord.Attributes().Get(logAttrName)
			if ok {
				logAttrs[j] = TopicAttr{
					Name:  logAttrName,
					Value: val,
				}
			} else {
				logAttrs[j] = TopicAttr{
					Name:  logAttrName,
					Value: pcommon.NewValueEmpty(),
				}
			}
		}

		// create hash of the resource and log attributes
		topicID := uint64(0)
		for _, v := range append(resourceAttrs, logAttrs...) {
			v, err := hashAttrValue(v.Value)
			if err != nil {
				return nil, err
			}
			topicID = hashCombine(topicID, v)
		}

		if _, ok := logsByTopic[topicID]; !ok {
			logsByTopic[topicID] = plog.NewLogRecordSlice()
		}
		newLog := logsByTopic[topicID].AppendEmpty()
		logRecord.MoveTo(newLog)

		topicManager.PutTopicMeta(
			topicID,
			logAttrs,
			resourceAttrs,
		)
	}

	return logsByTopic, nil
}

func toScopeLogsByTopic(
	scopeLogs plog.ScopeLogs,
	logsByTopic map[uint64]plog.LogRecordSlice,
) map[uint64]plog.ScopeLogs {
	// zero out the scope log's log records as we'll be copying it
	// and re-appending new log records
	scopeLogs.LogRecords().MoveAndAppendTo(plog.NewLogRecordSlice())

	result := map[uint64]plog.ScopeLogs{}
	for topicID, logs := range logsByTopic {
		result[topicID] = plog.NewScopeLogs()
		scopeLogs.CopyTo(result[topicID])
		logs.MoveAndAppendTo(result[topicID].LogRecords())
	}

	return result
}

func toScopeLogsSliceByTopic(
	scopeLogsByTopic ...map[uint64]plog.ScopeLogs,
) map[uint64]plog.ScopeLogsSlice {
	result := map[uint64]plog.ScopeLogsSlice{}
	for _, s := range scopeLogsByTopic {
		for topicID, scopeLog := range s {
			if _, ok := result[topicID]; !ok {
				result[topicID] = plog.NewScopeLogsSlice()
			}
			scopeLog.MoveTo(result[topicID].AppendEmpty())
		}
	}
	return result
}

func toResourceLogsByTopic(
	resourceLogs plog.ResourceLogs,
	scopeLogsByTopic map[uint64]plog.ScopeLogsSlice,
) map[uint64]plog.ResourceLogs {
	// zero out the resource log's scope logs as we'll be copying it
	// and re-appending new scope logs
	resourceLogs.ScopeLogs().MoveAndAppendTo(plog.NewScopeLogsSlice())

	result := map[uint64]plog.ResourceLogs{}
	for topicID, scopeLogSlice := range scopeLogsByTopic {
		result[topicID] = plog.NewResourceLogs()
		resourceLogs.CopyTo(result[topicID])
		scopeLogSlice.MoveAndAppendTo(result[topicID].ScopeLogs())
	}

	return result
}

func toResourceLogsSliceByTopic(
	resourceLogsByTopic ...map[uint64]plog.ResourceLogs,
) map[uint64]plog.ResourceLogsSlice {
	result := map[uint64]plog.ResourceLogsSlice{}
	for _, rl := range resourceLogsByTopic {
		for topicID, resourceLog := range rl {
			if _, ok := result[topicID]; !ok {
				result[topicID] = plog.NewResourceLogsSlice()
			}
			resourceLog.MoveTo(result[topicID].AppendEmpty())
		}
	}
	return result
}

func toLogsByTopic(
	logs plog.Logs,
	resourceLogsByTopic map[uint64]plog.ResourceLogsSlice,
) map[uint64]plog.Logs {
	// zero out the logs's resource logs as we'll be copying it
	// and re-appending new resource logs
	logs.ResourceLogs().MoveAndAppendTo(plog.NewResourceLogsSlice())

	result := map[uint64]plog.Logs{}
	for topicID, resourceLogSlice := range resourceLogsByTopic {
		result[topicID] = plog.NewLogs()
		logs.CopyTo(result[topicID])
		resourceLogSlice.MoveAndAppendTo(result[topicID].ResourceLogs())
	}
	return result
}

// https://stackoverflow.com/questions/35985960/c-why-is-boosthash-combine-the-best-way-to-combine-hash-values
func hashCombine(lhs, rhs uint64) uint64 {
	return lhs ^ (rhs + 0x9e3779b9 + (lhs << 6) + (lhs >> 2))
}

// hashAttrValue returns a unique hash of the attribute value including the type
// type included so that e.g.  pcommon.NewStringValue("A") and
// pcommon.NewByteSlice().Append(0x65) will have different hashes even though
// they contain the same bytes
func hashAttrValue(v pcommon.Value) (uint64, error) {
	hasher := fnv.New64a()

	bytes := make([]byte, 2)
	binary.BigEndian.PutUint16(bytes, uint16(v.Type()))
	hasher.Write(bytes)

	pcommon.NewByteSlice().Append()

	switch v.Type() {
	case pcommon.ValueTypeStr:
		hasher.Write([]byte(v.AsString()))

	case pcommon.ValueTypeBool:
		if v.Bool() {
			hasher.Write([]byte{1})
		} else {
			hasher.Write([]byte{0})
		}

	case pcommon.ValueTypeInt:
		bytes := make([]byte, 8)
		binary.BigEndian.PutUint64(bytes, uint64(v.Int()))
		hasher.Write(bytes)

	case pcommon.ValueTypeBytes:
		hasher.Write(v.Bytes().AsRaw())

	case pcommon.ValueTypeDouble:
		bytes := make([]byte, 8)
		binary.BigEndian.PutUint64(bytes, math.Float64bits(v.Double()))
		hasher.Write(bytes)

	case pcommon.ValueTypeEmpty:
		hasher.Write([]byte{0})

	case pcommon.ValueTypeMap:
		shardID := uint64(0)
		m := v.Map()
		var err error
		m.Range(func(k string, childV pcommon.Value) bool {
			hasher.Write([]byte(k))
			var childHash uint64
			childHash, err = hashAttrValue(childV)
			if err != nil {
				return false
			}
			shardID = hashCombine(shardID, childHash)
			bytes := make([]byte, 8)
			binary.BigEndian.PutUint64(bytes, shardID)
			hasher.Write(bytes)
			return true
		})
		if err != nil {
			return 0, err
		}

	case pcommon.ValueTypeSlice:
		shardID := uint64(0)
		for i := 0; i < v.Slice().Len(); i++ {
			childV := v.Slice().At(i)
			childHash, err := hashAttrValue(childV)
			if err != nil {
				return 0, err
			}
			shardID = hashCombine(shardID, childHash)
		}
		bytes := make([]byte, 8)
		binary.BigEndian.PutUint64(bytes, uint64(v.Int()))
		hasher.Write(bytes)

	default:
		return 0, fmt.Errorf("unknown value type: %v", v.Type())
	}

	return hasher.Sum64(), nil
}

func TopicWithIdKey(base string, id uint64) string {
	if id == 0 {
		return base
	}

	return fmt.Sprintf("%s_%d", base, id)
}

func TopicWithIdMetaKey(base string, id uint64) string {
	return TopicWithIdKey(base, id) + "_meta"
}

func TopicMetaKey(base string) string {
	return base + "_topics_meta"
}
