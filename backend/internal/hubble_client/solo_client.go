package hubble_client

import (
	"context"

	"github.com/cilium/cilium/api/v1/observer"
	"github.com/cilium/hubble-ui/backend/internal/flow_stream"
	"github.com/cilium/hubble-ui/backend/internal/statuschecker"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

type client struct {
	log         logrus.FieldLogger
	redisClient redis.UniversalClient
}

func NewSolo(
	log logrus.FieldLogger,
	redisClient redis.UniversalClient,
) *client {
	return &client{
		log:         log,
		redisClient: redisClient,
	}
}

func (c *client) FlowStream() flow_stream.FlowStreamInterface {
	getFlowsHandle, err := flow_stream.NewSolo(
		c.log.WithField("component", "FlowStream"),
		c.redisClient,
	)
	if err != nil {
		c.log.WithError(err).Error("Failed to build FlowStream handle, panic")
		panic(err.Error())
	}

	return getFlowsHandle
}

func (c *client) ServerStatus(context.Context) (*observer.ServerStatusResponse, error) {
	panic("not implemented")
	// return nil, nil
}

func (c *client) ServerStatusChecker(opts StatusCheckerOptions) (statuschecker.ServerStatusCheckerInterface, error) {
	panic("not implemented")
}
