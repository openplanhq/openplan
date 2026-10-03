import TemplateRunActions from "../runs/TemplateRunActions";
import TemplateRunHistory from "../runs/TemplateRunHistory";
import { useStackTemplate } from "./stackTemplateContext";

// /stacks/:stackId/templates/:stackTemplateId/runs — the default tab: a header
// with the actions that start a run, and the runs below it as one list.
export default function TemplateRunsTab() {
  const { stackId, stackTemplate } = useStackTemplate();
  return (
    <section className="grid min-w-0 grid-cols-1 content-start gap-6" data-testid="template-runs-tab">
      <TemplateRunActions stackId={stackId} stackTemplate={stackTemplate} />
      <TemplateRunHistory stackId={stackId} stackTemplateId={stackTemplate.id} />
    </section>
  );
}
