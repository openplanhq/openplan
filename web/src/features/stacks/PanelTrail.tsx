import { Link } from "react-router-dom";

// The crumb row at the top of a view nested in a tab, such as a run under
// Runs or Change revision under Settings: the tab as a link back, then this
// view as plain text, the current page. `name` names the navigation.
export default function PanelTrail({ name, parent, current }: { name: string; parent: { label: string; to: string }; current: string }) {
  return (
    <nav aria-label={name} className="flex items-center gap-1.5 text-sm text-muted-foreground">
      <Link to={parent.to} className="hover:text-foreground hover:underline">
        {parent.label}
      </Link>
      <span aria-hidden="true" className="text-separator">
        /
      </span>
      <span aria-current="page" className="text-foreground">
        {current}
      </span>
    </nav>
  );
}
