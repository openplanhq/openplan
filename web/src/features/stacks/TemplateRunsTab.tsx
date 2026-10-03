import TemplateRunActions from "../runs/TemplateRunActions";
import TemplateRunHistory from "../runs/TemplateRunHistory";
import TemplateRunNotices from "../runs/TemplateRunNotices";
import { useStackTemplate } from "./stackTemplateContext";

// A template's Runs tab, and the stack's index when no template is in the
// URL: the toolbar that starts a run, what waits on a person, then every run.
export default function TemplateRunsTab() {
  const { stackId, stackTemplate } = useStackTemplate();
  return (
    <section className="flex min-w-0 flex-col gap-5" data-testid="template-runs-tab">
      <TemplateRunActions stackId={stackId} stackTemplate={stackTemplate} />
      <TemplateRunNotices stackId={stackId} stackTemplate={stackTemplate} />
      <TemplateRunHistory stackId={stackId} stackTemplateId={stackTemplate.id} />
    </section>
  );
}
