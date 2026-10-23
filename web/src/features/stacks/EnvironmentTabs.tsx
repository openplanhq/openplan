import { useState } from "react";
import type { CredentialMetadata, CredentialRequest } from "../../api/types";
import SegmentedControl from "../../shared/SegmentedControl";
import CredentialsPanel from "./CredentialsPanel";

type EnvironmentTab = "secrets" | "variables";

interface TabCopy {
  note: string;
  emptyTitle: string;
  emptyDescription: string;
}

interface EnvironmentTabsProps {
  secrets: TabCopy;
  variables: TabCopy;
  credentials: CredentialMetadata[];
  loading: boolean;
  busy: boolean;
  onCreate: (credential: CredentialRequest) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
}

/**
 * An environment's Secrets and Variables, switched in place. One list comes
 * from the server; each entry's `sensitive` flag puts it in its tab, and each
 * tab's form adds entries of its own kind, so there is no flag to set.
 *
 * Both panels stay mounted and the other is hidden: a half-typed entry keeps
 * its tab when someone switches away and back, and a secret being typed can
 * never carry over into the Variables tab's plain-text box. A hidden draft
 * still marks the page unsaved for the re-auth guard.
 *
 * It opens on Secrets every time, so the tab someone lands on is the safe one.
 */
export default function EnvironmentTabs({ secrets, variables, credentials, loading, busy, onCreate, onDelete }: EnvironmentTabsProps) {
  const [tab, setTab] = useState<EnvironmentTab>("secrets");
  // Only an entry the server says is not sensitive is a variable; anything
  // else stays in Secrets, the side that never shows a value.
  const secretEntries = credentials.filter((credential) => credential.sensitive !== false);
  const variableEntries = credentials.filter((credential) => credential.sensitive === false);

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <SegmentedControl
        label="Environment"
        className="self-start"
        value={tab}
        onChange={setTab}
        options={[
          // Counts are left out until the list is known, as a tab's are.
          { value: "secrets", label: "Secrets", count: loading ? undefined : secretEntries.length },
          { value: "variables", label: "Variables", count: loading ? undefined : variableEntries.length }
        ]}
      />
      <div hidden={tab !== "secrets"} data-testid="environment-secrets">
        <CredentialsPanel kind="secret" {...secrets} credentials={secretEntries} loading={loading} busy={busy} onCreate={onCreate} onDelete={onDelete} />
      </div>
      <div hidden={tab !== "variables"} data-testid="environment-variables">
        <CredentialsPanel kind="variable" {...variables} credentials={variableEntries} loading={loading} busy={busy} onCreate={onCreate} onDelete={onDelete} />
      </div>
    </div>
  );
}
