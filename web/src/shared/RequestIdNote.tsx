import { ApiRequestError } from "../api/client";

// A server fault says nothing about its cause; the API logs the cause under
// the request id. Showing the id under a failure gives a person something an
// operator can search for. Anything else, a 4xx or a network failure, has no
// log line worth quoting, so this renders nothing.
export default function RequestIdNote({ error }: { error: unknown }) {
  if (!(error instanceof ApiRequestError) || error.status < 500 || error.requestId === "") {
    return null;
  }
  return (
    <p className="text-caption text-muted-foreground" data-testid="request-id">
      Request ID: <span className="font-mono select-all">{error.requestId}</span>
    </p>
  );
}
