import { useParams } from "react-router-dom";
import NoTemplatesState from "./NoTemplatesState";
import TemplatePanel from "./TemplatePanel";
import { useIndexTemplateId } from "./stackPageOutlet";
import TemplateRunsTab from "./TemplateRunsTab";

// /stacks/:stackId itself: the default template's panel on its Runs tab,
// drawn here rather than redirected to, so the URL stays the stack's and a
// phone can show the list at it. StackPage picks the template and keeps it;
// a stack with no templates says so.
export default function StackIndexPanel() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const selectedId = useIndexTemplateId();

  if (!selectedId) {
    return (
      <div className="p-7">
        <NoTemplatesState stackId={stackId} heading="h2" testId="stack-empty" />
      </div>
    );
  }
  return (
    <TemplatePanel stackTemplateId={selectedId}>
      <TemplateRunsTab />
    </TemplatePanel>
  );
}
