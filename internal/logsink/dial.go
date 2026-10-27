package logsink

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// readerPoolHeadroom is the reader pool's room beyond one connection
	// per tailer, for the backlog reads of subscribers joining them.
	readerPoolHeadroom = 32
	// readerPoolTimeout is how long a read waits for a free connection.
	// Short, so a full pool fails a viewer fast instead of hanging it; its
	// client reconnects.
	readerPoolTimeout = time.Second
)

// Dial connects to the Valkey at url, a redis:// or rediss:// URL, and pings
// it: the client connects lazily, and an unreachable server should stop
// startup rather than surface as the first run's lost output.
func Dial(ctx context.Context, url string) (*redis.Client, error) {
	return dial(ctx, url, func(*redis.Options) {})
}

// DialReaders is Dial for a Registry's client. Its pool fits MaxTailers
// blocking reads, and is apart from the writers' client: tailers pin a
// connection each, and on a shared pool they would starve the writes that
// end each log's stream.
func DialReaders(ctx context.Context, url string) (*redis.Client, error) {
	return dial(ctx, url, func(options *redis.Options) {
		options.PoolSize = MaxTailers + readerPoolHeadroom
		options.PoolTimeout = readerPoolTimeout
	})
}

func dial(ctx context.Context, url string, configure func(*redis.Options)) (*redis.Client, error) {
	options, err := redis.ParseURL(url)
	if err != nil {
		// url.Parse's errors quote the whole URL, password included.
		if urlErr, ok := errors.AsType[*neturl.Error](err); ok {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("parse valkey url: %w", err)
	}
	// The Valkey users may not SELECT, which go-redis sends on every new
	// connection for any other database: said here, rather than as a NOPERM
	// from the ping.
	if options.DB != 0 {
		return nil, fmt.Errorf("valkey url selects database %d; use database 0, since the Valkey users may not SELECT", options.DB)
	}
	// Without this, go-redis runs socket reads and writes under a background
	// context, and a caller's deadline, such as the flusher's per pass,
	// bounds only the wait for a connection.
	options.ContextTimeoutEnabled = true
	configure(options)
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping valkey %s: %w", options.Addr, err)
	}
	return client, nil
}
