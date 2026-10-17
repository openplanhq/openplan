package livelog

import (
	"context"
	"reflect"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/vishu42/openplan/internal/domain"
)

func TestStreamKeyHashTagsTheRun(t *testing.T) {
	t.Parallel()

	if got := StreamKey("run_123", "plan-1"); got != "runlog:{run_123}:plan-1" {
		t.Fatalf("StreamKey = %q, want runlog:{run_123}:plan-1", got)
	}
}

func TestEntryRoundTripsThroughValues(t *testing.T) {
	t.Parallel()

	for name, entry := range map[string]Entry{
		"piece":     {Kind: KindPiece, Offset: 1024, Data: []byte("hi\n")},
		"truncated": {Kind: KindTruncated},
		"end":       {Kind: KindEnd, State: domain.TemplateRunLogComplete},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseEntry(redis.XMessage{ID: "1-0", Values: entry.Values()})
			if err != nil {
				t.Fatalf("ParseEntry: %v", err)
			}
			want := entry
			want.ID = "1-0"
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("ParseEntry = %+v, want %+v", got, want)
			}
		})
	}
}

func TestParseEntryRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	if _, err := ParseEntry(redis.XMessage{ID: "1-0", Values: map[string]any{"kind": "x"}}); err == nil {
		t.Fatal("ParseEntry accepted kind x")
	}
}

func TestAppendEndWritesTheEntryAndShortensTheExpiry(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	key := StreamKey("run_123", "plan-1")

	// A live log's stream already carries the long expiry its writer set.
	if err := client.XAdd(ctx, &redis.XAddArgs{Stream: key, Values: Entry{Kind: KindTruncated}.Values()}).Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := client.Expire(ctx, key, InitialTTL).Err(); err != nil {
		t.Fatalf("seed expiry: %v", err)
	}

	if err := AppendEnd(ctx, client, key, domain.TemplateRunLogLost); err != nil {
		t.Fatalf("AppendEnd: %v", err)
	}

	messages, err := client.XRange(ctx, key, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRANGE: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("stream holds %d entries, want the seed and the end", len(messages))
	}
	end, err := ParseEntry(messages[1])
	if err != nil {
		t.Fatalf("ParseEntry: %v", err)
	}
	if end.Kind != KindEnd || end.State != domain.TemplateRunLogLost {
		t.Fatalf("last entry = %+v, want an end entry with state lost", end)
	}
	if ttl := server.TTL(key); ttl != EndTTL {
		t.Fatalf("TTL = %v, want %v", ttl, EndTTL)
	}
}
