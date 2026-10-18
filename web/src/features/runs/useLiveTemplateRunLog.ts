import { useEffect, useState } from "react";
import type { TemplateRunLogState } from "../../api/types";

export interface LiveTemplateRunLog {
  // Every piece received so far, in order. Kept apart rather than joined so a
  // caller can render each as its own text node: a new piece then adds a node
  // instead of replacing a text that can run to megabytes.
  pieces: readonly string[];
  // How much of the log the stream has carried, in the stored log's bytes:
  // the end the server gave the last piece. Against the stored log's size it
  // tells whether the stream missed anything.
  streamedBytes: number;
  // A piece started past the end of the one before it, so the bytes between
  // never streamed. The server never overlaps pieces, but it cannot replay
  // what its store lost: Valkey keeps nothing on disk, and after a restart
  // the executor carries on from its own offset.
  gap: boolean;
  // The output outgrew what the API streams live: no more text follows, and
  // the rest is only in the log's body once it is complete.
  truncated: boolean;
  // The state log_end reported, or null while the log is still followed.
  ended: TemplateRunLogState | null;
  // The browser gave up on the stream and will not reconnect.
  failed: boolean;
}

const NOTHING_YET: LiveTemplateRunLog = { pieces: [], streamedBytes: 0, gap: false, truncated: false, ended: null, failed: false };

const ENDED_STATES: readonly TemplateRunLogState[] = ["complete", "lost"];

// The event's JSON data, or null for data that does not parse: one malformed
// event is skipped rather than ending the view.
function eventData<T>(event: Event): T | null {
  try {
    return JSON.parse((event as MessageEvent<string>).data) as T;
  } catch {
    return null;
  }
}

// Follows a running command's log over server-sent events, from its first
// byte. EventSource reconnects on its own after a dropped connection, resending
// the last event id, and the server sends nothing this client already holds,
// so every piece is appended as it arrives. The source is closed once the log
// ends and when the caller unmounts.
//
// The state belongs to one stream: a caller that can point it at another log
// keys its component by url, so the new log starts from nothing.
export function useLiveTemplateRunLog(url: string): LiveTemplateRunLog {
  const [live, setLive] = useState(NOTHING_YET);

  useEffect(() => {
    // Same origin, so the session cookie travels with the request.
    const source = new EventSource(url);

    source.addEventListener("log", (event) => {
      const piece = eventData<{ data: string; offset?: number; end?: number }>(event);
      if (typeof piece?.data !== "string") {
        return;
      }
      // offset and end count raw bytes, which the text's replaced characters
      // would miscount, so they are taken as sent rather than counted here.
      const end = typeof piece.end === "number" ? piece.end : 0;
      setLive((current) => ({
        ...current,
        pieces: [...current.pieces, piece.data],
        streamedBytes: Math.max(current.streamedBytes, end),
        gap: current.gap || (typeof piece.offset === "number" && piece.offset > current.streamedBytes)
      }));
    });
    source.addEventListener("log_truncated", () => setLive((current) => ({ ...current, truncated: true })));
    source.addEventListener("log_end", (event) => {
      const state = eventData<{ state: TemplateRunLogState }>(event)?.state;
      // "live" means the server stopped following this client while the
      // command runs on; the browser reconnects by itself, so nothing ends.
      // A state this page does not know is read the same way.
      if (state === undefined || !ENDED_STATES.includes(state)) {
        return;
      }
      source.close();
      setLive((current) => ({ ...current, ended: state }));
    });
    source.addEventListener("error", () => {
      // An error while CONNECTING is a reconnect underway, not a failure.
      if (source.readyState === EventSource.CLOSED) {
        setLive((current) => ({ ...current, failed: true }));
      }
    });

    return () => source.close();
  }, [url]);

  return live;
}
