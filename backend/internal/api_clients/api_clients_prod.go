package api_clients

import (
	"context"

	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
	clientScheme "k8s.io/client-go/kubernetes/scheme"

	"github.com/cilium/hubble-ui/backend/internal/config"
	"github.com/cilium/hubble-ui/backend/internal/hubble_client"
	"github.com/cilium/hubble-ui/backend/internal/ns_watcher"
	"github.com/cilium/hubble-ui/backend/soloio/storage/remote"
)

var scheme = runtime.NewScheme()

func init() {
	_ = clientScheme.AddToScheme(scheme)
}

type APIClients struct {
	cfg *config.Config
	log logrus.FieldLogger

	// k8s    kubernetes.Interface
	// cilium *cilium.Clientset

	// // TODO: GRPCClient can be refactored to be a generalized connection pool
	// // for both Relay/Timescape clients
	// relayGrpc *grpc_client.GRPCClient

	redisClient    redis.UniversalClient
	snapshotReader remote.Reader
}

func New(
	ctx context.Context,
	cfg *config.Config,
	log logrus.FieldLogger,
) (*APIClients, error) {
	clients := &APIClients{
		cfg: cfg,
		log: log,
	}

	// k8sConfig, k8s, err := initK8sClientset()
	// if err != nil {
	// 	return nil, errors.Wrap(err, "k8s clientset init failed")
	// }

	// clients.k8s = k8s

	// ciliumClientset, err := initCiliumClientset(k8sConfig)
	// if err != nil {
	// 	return nil, errors.Wrap(err, "cilium clientset init failed")
	// }

	// clients.cilium = ciliumClientset

	// relayGrpc, err := initRelayGRPCClient(cfg, log.WithField("grpc-client", "relay"))
	// if err != nil {
	// 	return nil, errors.Wrap(err, "relay grpc client init failed")
	// }

	// clients.relayGrpc = relayGrpc

	redisClient := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{cfg.RedisAddress},
	})
	clients.redisClient = redisClient

	snapshotReader, err := startRedisReader(ctx, scheme, redisClient)
	if err != nil {
		return nil, errors.Wrap(err, "redis snapshot reader init failed")
	}
	clients.snapshotReader = snapshotReader

	return clients, nil
}

func (c *APIClients) NSWatcher(ctx context.Context, opts ns_watcher.NSWatcherOptions) (
	ns_watcher.NSWatcherInterface, error,
) {
	// return ns_watcher.New(opts.Log, c.k8s)
	return ns_watcher.NewSolo(opts.SnapshotReader)
}

func (c *APIClients) HubbleClient() hubble_client.HubbleClientInterface {
	return hubble_client.NewSolo(
		c.log.WithField("component", "hubble-client"),
		c.redisClient)
}

func (c *APIClients) SnapshotReader() remote.Reader {
	return c.snapshotReader
}

func startRedisReader(
	ctx context.Context,
	scheme *runtime.Scheme,
	redisClient redis.UniversalClient,
) (remote.Reader, error) {
	redisReader, err := remote.NewRedisPersistenceClient(
		ctx,
		redisClient,
		scheme,
		remote.Options{},
	)
	if err != nil {
		return nil, err
	}
	return redisReader, nil
}
