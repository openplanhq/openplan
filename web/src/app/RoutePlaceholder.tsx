import { Construction } from "lucide-react";
import RouteMessage from "./RouteMessage";

export default function RoutePlaceholder({ title }: { title: string }) {
  return (
    <RouteMessage
      icon={Construction}
      title={title}
      description="This screen has not been built yet."
      testId="route-placeholder"
    />
  );
}
