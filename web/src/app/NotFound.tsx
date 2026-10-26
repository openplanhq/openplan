import { SearchX } from "lucide-react";
import { Link } from "react-router-dom";
import { cn } from "@/lib/utils";
import { buttonClass } from "../shared/buttonClass";
import RouteMessage from "./RouteMessage";

export default function NotFound() {
  return (
    <RouteMessage
      icon={SearchX}
      title="Page not found"
      description="The page you were looking for doesn't exist."
      testId="route-not-found"
      action={
        <Link to="/stacks" className={cn(buttonClass("outline"), "pointer-coarse:h-11")}>
          Back to stacks
        </Link>
      }
    />
  );
}
