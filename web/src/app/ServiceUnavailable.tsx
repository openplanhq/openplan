import { ServerCrash } from "lucide-react";
import RouteMessage from "./RouteMessage";

export default function ServiceUnavailable() {
  return (
    <RouteMessage
      icon={ServerCrash}
      title="Authorization service unavailable"
      description="Authorization service unavailable — try again shortly."
      testId="route-service-unavailable"
    />
  );
}
