import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { isTerminalRegistrationStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useRegisterTemplateMutation, useTemplateRegistrationQuery, useTemplateRevisionsQuery } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { fieldLabelClass, inputClass } from "../../shared/fieldClass";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import { rootPathOf } from "./templateLinks";
import { templatePath } from "./templateRoutes";
import { sourceTemplateKey } from "./templateWorkflow";

const DEFAULTS = { owner: "hashicorp", repo: "", ref: "main", path: "." };

// /templates/new: Register template, inside the page's panel, in place of a
// template's header. Registration is asynchronous: the POST returns a
// registration, which is polled until it settles. While it runs the fields
// are locked and a status row says what is happening. When it completes, the
// template opens once the refreshed list holds its revision; an identity that
// was registered already opens that template. A failure keeps the fields as
// typed, says so, and shows the server's reason as written.
export default function RegisterTemplatePanel() {
  const [repoOwner, setRepoOwner] = useState(DEFAULTS.owner);
  const [repoName, setRepoName] = useState(DEFAULTS.repo);
  const [sourceRef, setSourceRef] = useState(DEFAULTS.ref);
  const [rootPath, setRootPath] = useState(DEFAULTS.path);
  const [registrationID, setRegistrationID] = useState("");
  const [requestError, setRequestError] = useState("");

  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const registerTemplateMutation = useRegisterTemplateMutation(tenantID);
  const registrationQuery = useTemplateRegistrationQuery(tenantID, registrationID);
  const revisions = useTemplateRevisionsQuery(tenantID).data ?? [];

  const registration = registrationQuery.data ?? null;
  const status = registration?.status ?? null;
  const settled = status !== null && isTerminalRegistrationStatus(status);
  const pollError =
    registrationID !== "" && registrationQuery.isError
      ? registrationQuery.error instanceof Error
        ? registrationQuery.error.message
        : "Request failed"
      : "";
  const registeredRevisionID = status === "completed" ? registration?.template_revision_id ?? "" : "";
  const registered = registeredRevisionID === "" ? null : revisions.find((templateRevision) => templateRevision.id === registeredRevisionID) ?? null;
  const registeredKey = registered ? sourceTemplateKey(registered) : "";
  // Completed, but the refetched list does not hold the revision yet, so the
  // template cannot open: still registering, as far as anyone can tell.
  const opening = registeredRevisionID !== "" && registered === null;

  const busy = registerTemplateMutation.isPending || (registrationID !== "" && !settled && pollError === "") || opening;
  // A registration that completes without a revision registered nothing.
  const failed = settled && (status !== "completed" || registeredRevisionID === "");
  const failure = requestError || pollError || (failed ? registration?.error_summary || "Registration failed" : "");
  // A poll that fails says nothing about the registration itself, which may
  // still finish; registering again now could race it.
  const unchecked = requestError === "" && pollError !== "";

  useEffect(() => {
    if (registeredRevisionID === "") {
      return;
    }
    // The workflow writes the revision; only a refetch of the list shows it.
    void queryClient.invalidateQueries({ queryKey: queryKeys.templateRevisions(tenantID) });
  }, [registeredRevisionID, queryClient]);

  useEffect(() => {
    if (registeredKey !== "") {
      navigate(templatePath(registeredKey), { replace: true });
    }
  }, [registeredKey, navigate]);

  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so a half-filled
  // form is never wiped out by a background sign-in redirect.
  const hasUnsaved = repoOwner !== DEFAULTS.owner || repoName !== DEFAULTS.repo || sourceRef !== DEFAULTS.ref || rootPath !== DEFAULTS.path;
  const path = rootPathOf(rootPath);
  const target = `${repoOwner.trim()}/${repoName.trim()}${path === null ? "" : ` · ${path}`} at ${sourceRef.trim()}`;

  async function handleRegister(event: FormEvent) {
    event.preventDefault();
    if (busy) {
      return;
    }
    setRequestError("");
    // Drop any earlier attempt, so a retry polls its own registration.
    setRegistrationID("");
    try {
      const next = await registerTemplateMutation.mutateAsync({ repo_owner: repoOwner, repo_name: repoName, source_ref: sourceRef, root_path: rootPath });
      setRegistrationID(next.id);
    } catch (error) {
      setRequestError(error instanceof Error ? error.message : "Request failed");
    }
  }

  return (
    <section className="flex min-w-0 flex-col" data-testid="register-template-panel" data-unsaved={hasUnsaved ? "true" : undefined}>
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3 border-b border-divider px-7 py-6">
        <div className="flex min-w-0 flex-col gap-2">
          <h2 className="font-heading text-panel-title font-semibold tracking-title">Register template</h2>
          <p className="text-meta text-muted-foreground">from a Git repository</p>
        </div>
        <Link to="/templates" className={cn(buttonClass("outline", "lg"), "pointer-coarse:h-11")} data-testid="register-template-cancel">
          Cancel
        </Link>
      </div>
      <form className="flex max-w-140 min-w-0 flex-col gap-5 px-7 pt-5 pb-7" onSubmit={(event) => void handleRegister(event)}>
        <p className="text-meta text-muted-foreground">
          openplan clones the repository, reads the module's variables and its <span className="font-mono text-code-foreground">template.yaml</span>, and
          registers the commit the ref points at.
        </p>
        <div className="flex flex-wrap gap-x-2 gap-y-4">
          <Field id="template-repo-owner" label="Owner" hint="The GitHub organization or user." value={repoOwner} onChange={setRepoOwner} disabled={busy} grow />
          <Field id="template-repo-name" label="Repository" hint="The repository name." value={repoName} onChange={setRepoName} disabled={busy} grow />
        </div>
        <Field
          id="template-source-ref"
          label="Ref"
          hint="A branch or a tag. On a branch, Sync picks up new commits."
          value={sourceRef}
          onChange={setSourceRef}
          disabled={busy}
        />
        <Field
          id="template-root-path"
          label="Root path"
          hint="The module's directory in the repository, or . when the module is at its root."
          value={rootPath}
          onChange={setRootPath}
          disabled={busy}
        />
        <button type="submit" className={cn(buttonClass("primary"), "self-start pointer-coarse:h-11")} disabled={busy} data-testid="register-template-submit">
          {busy ? <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" /> : <Plus data-icon="inline-start" aria-hidden="true" />}
          Register template
        </button>
        {busy && (
          <div role="status" className="flex items-start gap-3 rounded-lg border px-5 py-4" data-testid="register-template-progress">
            <Loader2 aria-hidden="true" className="mt-0.5 size-4 shrink-0 animate-spin text-muted-foreground" />
            <span className="flex min-w-0 flex-col gap-1">
              <span className="text-sm leading-label font-medium">
                Registering <span className="font-mono text-meta wrap-anywhere">{target}</span>
              </span>
              <span className="text-meta text-muted-foreground">Cloning the repository and reading the module. The template opens here when it is registered.</span>
            </span>
          </div>
        )}
        {failure !== "" && !busy && (
          <div className="flex flex-col gap-2" data-testid="register-template-error">
            <ErrorLine>
              {unchecked
                ? "openplan could not check on this registration, so it may still finish. Reload the page before registering it again."
                : "This template could not be registered. Check the fields and register it again."}
            </ErrorLine>
            <pre className="m-0 rounded-lg border bg-canvas px-3 py-2.5 font-mono text-xs leading-relaxed whitespace-pre-wrap text-code-foreground wrap-anywhere">
              {failure}
            </pre>
          </div>
        )}
      </form>
    </section>
  );
}

// openplan UI's Field: a label over its control over what it is for. Owner
// and Repository grow side by side and wrap on a phone.
function Field({
  id,
  label,
  hint,
  value,
  onChange,
  disabled,
  grow = false
}: {
  id: string;
  label: string;
  hint: string;
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
  grow?: boolean;
}) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", grow && "flex-1 basis-50")}>
      <Label htmlFor={id} className={fieldLabelClass}>
        {label}
      </Label>
      <Input id={id} value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} aria-describedby={`${id}-hint`} className={inputClass} />
      <p id={`${id}-hint`} className="text-xs text-muted-foreground">
        {hint}
      </p>
    </div>
  );
}
