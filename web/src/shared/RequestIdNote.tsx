import { quotableRequestId } from "../api/problems";

// The request id under a failure, when it has one worth quoting; otherwise
// nothing.
export default function RequestIdNote({ error }: { error: unknown }) {
  const requestId = quotableRequestId(error);
  if (requestId === "") {
    return null;
  }
  return (
    <p className="text-caption text-muted-foreground">
      Request ID: <span className="font-mono select-all">{requestId}</span>
    </p>
  );
}
