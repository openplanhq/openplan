import { useParams } from "react-router-dom";
import { useStackQuery } from "../../api/queries";
import { tenantID } from "../../config";
import NoTemplatesState from "./NoTemplatesState";
import TemplatePanel from "./TemplatePanel";
import TemplateRunsTab from "./TemplateRunsTab";
import { defaultStackTemplate } from "./templateSelection";

// /stacks/:stackId itself: the default template's panel on its Runs tab,
// drawn here rather than redirected to, so the URL stays the stack's and a
// phone can show the list at it. A stack with no templates says so.
export default function StackIndexPanel() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const templates = useStackQuery(tenantID, stackId).data?.templates ?? [];
  const selected = defaultStackTemplate(templates);

  if (!selected) {
    return (
      <div className="p-7">
        <NoTemplatesState stackId={stackId} heading="h2" testId="stack-empty" />
      </div>
    );
  }
  return (
    <TemplatePanel stackTemplateId={selected.id}>
      <TemplateRunsTab />
    </TemplatePanel>
  );
}
