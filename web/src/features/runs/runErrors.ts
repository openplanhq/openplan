import { ApiRequestError } from "../../api/client";
import { problemTypes } from "../../api/types";

// The server refuses a second run while one is still in flight (409
// run-in-flight). Both run controls disable their buttons on the same rule, so
// reaching this response means the client's run history is behind the server's
// — another tab, or another user, started a run since this screen last polled.
// The caller answers by refetching, which puts the buttons back in the state
// the server already believes they are in.
export function isRunInFlightError(error: unknown): boolean {
  return error instanceof ApiRequestError && error.type === problemTypes.runInFlight;
}
