# Operations runbook

What an operator does when something goes wrong. Each section says how to
recognise the case, what is and isn't at risk, and the steps that recover
from it. Sections are added as the cases get an answer in the product
(openplanhq/openplan#320 tracks the rest).

## A run stuck in progress

A run that says `queued` or `running` and never moves. Its page shows the
same step for far longer than any Terraform command should take, nothing
new reaches its logs, and the template refuses every other run with
"Run #N is still running. Wait for it to finish or discard it."

A run is carried by a Temporal workflow, and only that workflow moves the
run on. So a stuck run is one of two things, and the Temporal UI tells
them apart. With the Compose stack it is at http://localhost:8080 under
`docker compose --profile debug up -d`; look up the workflow by id:

- `template-run/<tenant id>/<run id>` carries the run's plan, and
- `template-run/<tenant id>/<run id>/apply` carries its apply.

The run's page is addressed by its number; its id comes from the API:
`GET /v1/tenants/{tenant_id}/stack-templates/{stack_template_id}/runs/{run_number}`
returns it as `id`, and its `executions` list each workflow's id next to
the phase it carried.

### The workflow is still running

Temporal shows the workflow as Running. The run is not lost; something it
is waiting for is not happening.

- **No control worker is polling.** The api runs the control worker. If
  the api was down or restarting, the workflow resumes on its own when it
  is back, and the run catches up. Nothing to do but start the api; see
  "When the control worker is down" in [apply-run-sequence.md](apply-run-sequence.md).
- **No executor is available.** The run sits at `waiting for an executor`.
  A plan waits one minute and an apply ten for an executor before it fails
  on its own; if the executor is down, start it.
- **The Terraform command itself hangs**, waiting on a provider or a lock.
  Each command has a timeout (`OPENPLAN_TERRAFORM_TIMEOUT`, 45 minutes by
  default) after which the run fails on its own. If you cannot wait,
  terminate the workflow in the Temporal UI (Terminate, with a reason).
  That stops the workflow without letting it record anything, so the run
  stays in progress: it is now the second case below, and you end it there.

Do not end a run whose workflow is running: openplan refuses it anyway
(`409 run-workflow-running`), because the workflow would write over your
end.

### The workflow is gone

Temporal has no workflow under the run's id, or shows it as Completed,
Failed, Terminated or Timed out while openplan still says the run is
running. This happens after:

- a restore of Temporal's database from before the run started, or a crash
  that lost it: the workflow is gone;
- a restore of openplan's Postgres from before the run finished, while
  Temporal kept running: the workflow finished, but this database never
  heard;
- a workflow you terminated in the Temporal UI (above).

Nothing will ever move this run, so end it:

1. Open the run's page. Someone with operator access on the stack (or a
   platform admin) sees **End run** next to the run's status. Press it,
   read the question, and confirm. From a script, it is
   `POST /v1/tenants/{tenant_id}/template-runs/{run_id}/end` with an
   optional `{"reason": "..."}` body; see [the API reference](openapi.yaml).
2. The run ends `failed`. Its summary says an operator ended it and what
   Temporal held of its workflow: "was not found in Temporal", or "had
   already ended in Temporal (completed)" and so on. Its saved plan, if it had one, can no longer be
   applied, and the template accepts a new run.
3. **Plan again before trusting the stack.** Ending a run undoes nothing
   it did. If its apply had started, some of the resources may have been
   created or changed before the workflow was lost, and Terraform's state
   may or may not know. The next plan shows what is real; read it before
   applying anything.

If End run answers `503 workflows-unavailable`, openplan could not ask
Temporal and changed nothing. Check that the api can reach Temporal
(`TEMPORAL_ADDRESS`) and try again.
