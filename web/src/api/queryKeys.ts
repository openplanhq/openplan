export const queryKeys = {
  me: ["me"] as const,
  stacks: (tenantID: string) => ["stacks", tenantID] as const,
  // One filter's pages of the stacks list; invalidating stacks() reaches every one.
  stackList: (tenantID: string, q: string) => ["stacks", tenantID, q] as const,
  attention: (tenantID: string) => ["attention", tenantID] as const,
  templateRevisions: (tenantID: string) => ["templateRevisions", tenantID] as const,
  stack: (tenantID: string, stackID: string) => ["stack", tenantID, stackID] as const,
  stackCredentials: (tenantID: string, stackID: string) => ["stackCredentials", tenantID, stackID] as const,
  stackTemplateCredentials: (tenantID: string, stackTemplateID: string) => ["stackTemplateCredentials", tenantID, stackTemplateID] as const,
  templateRegistration: (tenantID: string, registrationID: string) =>
    ["templateRegistration", tenantID, registrationID] as const,
  templateRevisionVariables: (tenantID: string, templateRevisionID: string) =>
    ["templateRevisionVariables", tenantID, templateRevisionID] as const,
  templateRun: (tenantID: string, runID: string) => ["templateRun", tenantID, runID] as const,
  templateRuns: (tenantID: string, stackTemplateID: string) => ["templateRuns", tenantID, stackTemplateID] as const,
  templateRunByNumber: (tenantID: string, stackTemplateID: string, runNumber: string) =>
    ["templateRunByNumber", tenantID, stackTemplateID, runNumber] as const,
  templateRunLogs: (tenantID: string, runID: string, statusTag: string) =>
    ["templateRunLogs", tenantID, runID, statusTag] as const,
  // The list is recorded once, when the plan finishes, so the key carries
  // its state: it is read again only when that moves.
  templateRunChanges: (tenantID: string, runID: string, state: string) => ["templateRunChanges", tenantID, runID, state] as const,
  templateRunLog: (tenantID: string, runID: string, name: string) => ["templateRunLog", tenantID, runID, name] as const,
  stackGrants: (tenantID: string, stackID: string) => ["stackGrants", tenantID, stackID] as const,
  platformGrants: (tenantID: string) => ["platformGrants", tenantID] as const,
  // The query stays at [2]: useSearchUsersQuery's placeholder reads it there.
  userSearch: (tenantID: string, stackID: string, query: string) => ["userSearch", tenantID, query, stackID] as const
};
