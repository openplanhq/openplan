package livelog

import (
	"context"
	"errors"
	"fmt"
	neturl "net/url"

	"github.com/redis/go-redis/v9"
)

// Dial connects to the Valkey at url, a redis:// or rediss:// URL, and pings
// it: the client connects lazily, and an unreachable server should stop
// startup rather than surface as the first run's lost output.
func Dial(ctx context.Context, url string) (*redis.Client, error) {
	options, err := redis.ParseURL(url)
	if err != nil {
		// url.Parse's errors quote the whole URL, password included.
		if urlErr, ok := errors.AsType[*neturl.Error](err); ok {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("parse live log url: %w", err)
	}
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping live log server %s: %w", options.Addr, err)
	}
	return client, nil
}
