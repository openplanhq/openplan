import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Loader2, Send } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { isTerminalRegistrationStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useRegisterTemplateMutation, useTemplateRegistrationQuery } from "../../api/queries";
import { tenantID } from "../../config";
import Breadcrumb from "../../shared/Breadcrumb";
import StatusRow from "../../shared/StatusRow";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// /templates/new owns the register-then-poll flow. Registration is
// asynchronous: the POST only returns a registration ID, and the revision the
// list screen needs to highlight does not exist until polling reports
// "completed". So the screen stays put and reports progress, then redirects to
// /templates?selected=<revisionID> once there is something to select. A
// terminal failure keeps the user here with the inputs intact to correct.
export default function TemplateRegistrationScreen() {
  const [repoOwner, setRepoOwner] = useState("hashicorp");
  const [repoName, setRepoName] = useState("");
  const [sourceRef, setSourceRef] = useState("main");
  const [rootPath, setRootPath] = useState(".");
  const [registrationID, setRegistrationID] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const registrationQuery = useTemplateRegistrationQuery(tenantID, registrationID);
  const registerTemplateMutation = useRegisterTemplateMutation(tenantID);

  const registration = registrationQuery.data ?? null;
  const registrationStatus = registration?.status ?? null;

  useEffect(() => {
    const data = registrationQuery.data;
    if (data?.status !== "completed" || !data.template_revision_id) {
      return;
    }
    queryClient.invalidateQueries({ queryKey: queryKeys.templateRevisions(tenantID) });
    navigate(`/templates?selected=${encodeURIComponent(data.template_revision_id)}`, { replace: true });
  }, [registrationQuery.data?.status, registrationQuery.data?.template_revision_id, queryClient, navigate]);

  // Busy spans the whole flow, not just the POST: the request is in flight, or
  // a registration is being polled and has not reached a terminal status.
  const settled = registrationStatus !== null && isTerminalRegistrationStatus(registrationStatus);
  const busy = registerTemplateMutation.isPending || (registrationID !== "" && !settled);

  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so a half-filled
  // registration form is never wiped out by a background sign-in redirect.
  const hasUnsavedRegistration =
    repoOwner !== "hashicorp" || repoName !== "" || sourceRef !== "main" || rootPath !== ".";

  async function handleRegister(event: FormEvent) {
    event.preventDefault();
    setErrorMessage("");
    // Drop any previous attempt so a retry after a failure polls the new
    // registration rather than reading the old terminal one.
    setRegistrationID("");
    try {
      const next = await registerTemplateMutation.mutateAsync({
        repo_owner: repoOwner,
        repo_name: repoName,
        source_ref: sourceRef,
        root_path: rootPath
      });
      setRegistrationID(next.id);
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
    }
  }

  return (
    <section className="grid min-w-0 gap-6 text-foreground" data-unsaved={hasUnsavedRegistration ? "true" : undefined}>
      <Breadcrumb
        items={[
          { label: "Templates", to: "/templates", testId: "template-registration-back" },
          { label: "Register template" }
        ]}
      />

      <Card className="gap-0">
        <CardContent className="grid gap-6 p-4">
          {errorMessage && (
            <Alert variant="destructive" data-testid="template-registration-error">
              <AlertDescription>{errorMessage}</AlertDescription>
            </Alert>
          )}

          <form className="grid gap-4 md:grid-cols-2" onSubmit={handleRegister}>
            <div className="grid gap-2">
              <Label htmlFor="template-repo-owner">Owner</Label>
              <Input
                id="template-repo-owner"
                className="pointer-coarse:h-11"
                value={repoOwner}
                onChange={(event) => setRepoOwner(event.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="template-repo-name">Repository</Label>
              <Input
                id="template-repo-name"
                className="pointer-coarse:h-11"
                value={repoName}
                onChange={(event) => setRepoName(event.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="template-source-ref">Ref</Label>
              <Input
                id="template-source-ref"
                className="pointer-coarse:h-11"
                value={sourceRef}
                onChange={(event) => setSourceRef(event.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="template-root-path">Root path</Label>
              <Input
                id="template-root-path"
                className="pointer-coarse:h-11"
                value={rootPath}
                onChange={(event) => setRootPath(event.target.value)}
              />
            </div>
            <Button className="justify-self-start pointer-coarse:h-11 md:col-span-2" disabled={busy} type="submit">
              {busy ? <Loader2 className="size-4 animate-spin" /> : <Send className="size-4" />}
              Register
            </Button>
          </form>

          <StatusRow label="Registration" value={registrationStatus ?? "not started"} />
          {registration?.error_summary && (
            <Alert variant="destructive">
              <AlertDescription>{registration.error_summary}</AlertDescription>
            </Alert>
          )}
        </CardContent>
      </Card>
    </section>
  );
}
