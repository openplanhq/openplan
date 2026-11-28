<!--
What this pull request changes, in a sentence a reviewer who has not read the
issue can act on.

Delete the guidance below as it applies; what matters is that each section that
applies is actually filled in, not that the template's headings survive.
-->

## What this changes

<!-- The behavior change, not the diff. -->

## Why

<!-- The problem it solves, and the issue or design doc it traces to. -->

Closes #

## Configuration or migration impact

<!--
Delete if there is none. If yes, be specific: new environment variables and
their defaults (see .env.example), a migration that runs on boot, a change to
the Compose stack, anything an operator has to do by hand.
-->

- [ ] No configuration, migration, or deployment impact

## How this was verified

<!--
The commands you ran and their result. CI runs these, but state what you ran
locally -- it is the difference between "the suite passes" and "I ran the suite".

Backend, from the repository root:

    go build ./...
    go test ./...
    gofmt -l $(git ls-files '*.go')     # must print nothing

Frontend, from web/:

    npm ci && npm test && npm run build

Lint, from the repository root:

    make lint

Touching anything under internal/authorization/ or bumping
github.com/openfga/openfga:

    make differential-test                 # needs docker compose up -d postgres

The last one is not optional in that case. internal/authorization/write.go is a
transcription of OpenFGA's write path, it diverges silently when the dependency
moves, and TestTransactionalWriteMatchesUpstream is the only thing that catches
it. A skipped guard is no guard.
-->

## Screenshots

<!--
Required when frontend behavior or layout changes. Before and after if you can;
the current screenshots in README.md are the baseline. Delete if no UI change.
-->

## Notes for the reviewer

<!--
What you want looked at hardest, what you were unsure about, and anything you
deliberately left out of this PR.
-->
