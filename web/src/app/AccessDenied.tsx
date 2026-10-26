import { LockKeyhole } from "lucide-react";
import { Link } from "react-router-dom";
import { cn } from "@/lib/utils";
import { buttonClass } from "../shared/buttonClass";
import RouteMessage from "./RouteMessage";

export default function AccessDenied() {
  return (
    <RouteMessage
      icon={LockKeyhole}
      title="Not permitted"
      description="You don't have permission to do this."
      testId="route-access-denied"
      action={
        <Link to="/stacks" className={cn(buttonClass("outline"), "pointer-coarse:h-11")}>
          Back to stacks
        </Link>
      }
    />
  );
}
