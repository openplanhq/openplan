import AddStackTemplateScreen from "./AddStackTemplateScreen";

// templates/new in the stack's panel. The screen inside keeps its own look
// until it is rebuilt on openplan UI; this gives it the panel's padding.
export default function AddTemplatePanel() {
  return (
    <div className="flex min-w-0 flex-col px-7 pt-6 pb-7">
      <AddStackTemplateScreen />
    </div>
  );
}
