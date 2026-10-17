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
}

function nothingYet(url: string): Followed {
  return { url, text: "", truncated: false, ended: null, failed: false };
}

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
      const piece = eventData<{ data: string }>(event);
      if (typeof piece?.data !== "string") {
        return;
      }
      update((current) => ({ ...current, text: current.text + piece.data }));
    });
    source.addEventListener("log_truncated", () => update((current) => ({ ...current, truncated: true })));
    source.addEventListener("log_end", (event) => {
      const state = eventData<{ state: TemplateRunLogState }>(event)?.state;
      // "live" means the server stopped following this client while the
      // command runs on; the browser reconnects by itself, so nothing ends.
      // A state this page does not know is read the same way.
      if (state === undefined || !ENDED_STATES.includes(state)) {
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
