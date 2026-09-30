import { SearchX } from "lucide-react";
import RouteMessage from "./RouteMessage";

export default function NotFound() {
  return (
    <RouteMessage
      icon={SearchX}
      title="Page not found"
      description="The page you were looking for doesn't exist."
      testId="route-not-found"
    />
  );
}
