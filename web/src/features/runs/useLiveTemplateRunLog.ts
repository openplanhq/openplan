import { useEffect, useState } from "react";
import type { TemplateRunLogState } from "../../api/types";

export interface LiveTemplateRunLog {
  // Everything received so far.
  text: string;
  // The output outgrew what the API streams live: no more text follows, and
  // the rest is only in the log's body once it is complete.
  truncated: boolean;
  // The state log_end reported, or null while the log is still followed.
  ended: TemplateRunLogState | null;
  // The browser gave up on the stream and will not reconnect.
  failed: boolean;
}

interface Followed extends LiveTemplateRunLog {
  // The stream this state belongs to, so a new URL starts from nothing.
  url: string;
  // Bytes of the log received, as the server counts them in its offsets.
  received: number;
}

const encoder = new TextEncoder();

function nothingYet(url: string): Followed {
  return { url, received: 0, text: "", truncated: false, ended: null, failed: false };
}

// Follows a running command's log over server-sent events, from its first
// byte. EventSource reconnects on its own after a dropped connection, resending
// the last event id, so the stream picks up where it left off. The source is
// closed once the log ends and when the caller unmounts.
export function useLiveTemplateRunLog(url: string): LiveTemplateRunLog {
  const [followed, setFollowed] = useState(() => nothingYet(url));

  useEffect(() => {
    // Same origin, so the session cookie travels with the request.
    const source = new EventSource(url);
    // Updates apply to this URL's state, starting over if the last render
    // still held another stream's.
    const update = (next: (current: Followed) => Followed) =>
      setFollowed((current) => next(current.url === url ? current : nothingYet(url)));

    source.addEventListener("log", (event) => {
      const piece = JSON.parse((event as MessageEvent<string>).data) as { offset: number; data: string };
      update((current) => {
        // A reconnect can replay a piece already shown. Offsets count bytes,
        // not UTF-16 units, so the received count is kept in bytes too, and
        // resynced to the server's offset with every piece.
        if (piece.offset < current.received) {
          return current;
        }
        return { ...current, text: current.text + piece.data, received: piece.offset + encoder.encode(piece.data).length };
      });
    });
    source.addEventListener("log_truncated", () => update((current) => ({ ...current, truncated: true })));
    source.addEventListener("log_end", (event) => {
      const { state } = JSON.parse((event as MessageEvent<string>).data) as { state: TemplateRunLogState };
      // "live" means the server stopped following this client while the
      // command runs on; the browser reconnects by itself, so nothing ends.
      if (state === "live") {
        return;
      }
      source.close();
      update((current) => ({ ...current, ended: state }));
    });
    source.addEventListener("error", () => {
      // An error while CONNECTING is a reconnect underway, not a failure.
      if (source.readyState === EventSource.CLOSED) {
        update((current) => ({ ...current, failed: true }));
      }
    });

    return () => source.close();
  }, [url]);

  const current = followed.url === url ? followed : nothingYet(url);
  return { text: current.text, truncated: current.truncated, ended: current.ended, failed: current.failed };
}
