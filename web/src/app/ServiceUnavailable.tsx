import { ServerCrash } from "lucide-react";
import RouteMessage from "./RouteMessage";

export default function ServiceUnavailable() {
  return (
    <RouteMessage
      icon={ServerCrash}
      title="Service unavailable"
      description="openplan can't do this right now. Contact your operator."
      testId="route-service-unavailable"
    />
  );
}
