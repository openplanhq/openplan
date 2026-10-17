package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/livelog"
)

// LiveLogSubscriber follows a live log's stream. *livelog.Registry satisfies
// it; handler tests use a fake.
type LiveLogSubscriber interface {
	Subscribe(ctx context.Context, key, afterID string) (<-chan livelog.Entry, error)
}

var _ LiveLogSubscriber = (*livelog.Registry)(nil)

// handleStreamTemplateRunLog streams a run log as server-sent events while
// its command runs. Authorization is the row read's, as for the body.
func (server *Server) handleStreamTemplateRunLog(response http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	command := app.GetTemplateRunLogCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		RunID:    domain.TemplateRunID(request.PathValue("run_id")),
		Name:     request.PathValue("name"),
	}
	runLog, err := server.service.GetTemplateRunLogMetadata(ctx, command)
	if err != nil {
		writeAppError(response, err)
		return
	}
	// Checked before subscribing: without flushing, events would sit in a
	// buffer until the command ends, which is no live view at all.
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeInternalError(response, errors.New("live log stream: response writer cannot flush"))
		return
	}
	if runLog.State != domain.TemplateRunLogLive {
		stream := startEventStream(response, flusher)
		_ = stream.end(runLog.State)
		return
	}
	if server.liveLogs == nil {
		writeProblem(response, problemDocument{
			Type: problemTypeURI("live-logs-unavailable"), Title: "Live logs aren't configured",
			Status: http.StatusServiceUnavailable, Detail: "The API was started without live logs. Read the log once its command finishes.",
		})
		return
	}

	entries, err := server.liveLogs.Subscribe(ctx, livelog.StreamKey(command.RunID, command.Name), request.Header.Get("Last-Event-ID"))
	if errors.Is(err, livelog.ErrTooManyTailers) {
		writeProblem(response, problemDocument{
			Type: problemTypeURI("live-logs-busy"), Title: "Too many logs are being followed",
			Status: http.StatusServiceUnavailable, Detail: "This server is following as many live logs as it can. Try again shortly.",
		})
		return
	}
	if err != nil {
		if ctx.Err() == nil {
			writeInternalError(response, err)
		}
		return
	}

	server.streamEntries(ctx, startEventStream(response, flusher), entries, command)
}

// streamEntries sends each entry as its event until the log ends, the
// channel closes, or the client goes away.
func (server *Server) streamEntries(ctx context.Context, stream eventStream, entries <-chan livelog.Entry, command app.GetTemplateRunLogCommand) {
	// sent is how much of the log this response has carried. A retried
	// append repeats bytes under a new id, so a piece is cut to what lies
	// past it.
	var sent int64
	for {
		var (
			entry livelog.Entry
			open  bool
		)
		select {
		case <-ctx.Done():
			return
		case entry, open = <-entries:
		}
		if !open {
			server.endClosedStream(ctx, stream, command)
			return
		}
		var err error
		switch entry.Kind {
		case livelog.KindPiece:
			end := entry.Offset + int64(len(entry.Data))
			if end <= sent {
				continue
			}
			data, offset := entry.Data, entry.Offset
			if offset < sent {
				data, offset = data[sent-offset:], sent
			}
			sent = end
			err = stream.send("log", entry.ID, logEventData{Offset: offset, Data: strings.ToValidUTF8(string(data), "\uFFFD")})
		case livelog.KindTruncated:
			err = stream.send("log_truncated", "", struct{}{})
		case livelog.KindEnd:
			_ = stream.end(entry.State)
			return
		}
		if err != nil {
			return
		}
	}
}

// endClosedStream ends a stream whose channel closed without an end entry:
// the subscriber fell behind, or its tailer stopped. The row says what
// happened since; if the log is still live, the client reconnects from its
// last event.
func (server *Server) endClosedStream(ctx context.Context, stream eventStream, command app.GetTemplateRunLogCommand) {
	if ctx.Err() != nil {
		return
	}
	runLog, err := server.service.GetTemplateRunLogMetadata(ctx, command)
	if err != nil {
		// The 200 is already sent; dropping the connection makes the client
		// reconnect, which reads the row again.
		log.Printf("api: live log stream: re-read %s of run %s: %v", command.Name, command.RunID, err)
		return
	}
	_ = stream.end(runLog.State)
}

type logEventData struct {
	Offset int64  `json:"offset"`
	Data   string `json:"data"`
}

type logEndData struct {
	State domain.TemplateRunLogState `json:"state"`
}

// eventStream writes server-sent events, flushing each so it reaches the
// browser as it happens.
type eventStream struct {
	response http.ResponseWriter
	flusher  http.Flusher
}

func startEventStream(response http.ResponseWriter, flusher http.Flusher) eventStream {
	header := response.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	// nginx buffers proxied responses by default, which would hold events
	// back; this header turns that off for this response alone.
	header.Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	flusher.Flush()
	return eventStream{response: response, flusher: flusher}
}

// send writes one event. id is left out when empty. The error is the
// client's connection failing, after which nothing more can be sent.
func (stream eventStream) send(event, id string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode %s event: %w", event, err)
	}
	var frame strings.Builder
	frame.WriteString("event: " + event + "\n")
	if id != "" {
		frame.WriteString("id: " + id + "\n")
	}
	frame.WriteString("data: ")
	frame.Write(payload)
	frame.WriteString("\n\n")
	if _, err := stream.response.Write([]byte(frame.String())); err != nil {
		return fmt.Errorf("write %s event: %w", event, err)
	}
	stream.flusher.Flush()
	return nil
}

func (stream eventStream) end(state domain.TemplateRunLogState) error {
	return stream.send("log_end", "", logEndData{State: state})
}
