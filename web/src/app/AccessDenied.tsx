import { LockKeyhole } from "lucide-react";
import RouteMessage from "./RouteMessage";

export default function AccessDenied() {
  return (
    <RouteMessage
      icon={LockKeyhole}
      title="Not permitted"
      description="You don't have permission to do this."
      testId="route-access-denied"
    />
  );
}
