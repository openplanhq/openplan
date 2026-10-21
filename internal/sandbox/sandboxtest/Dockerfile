# The executor image plus the sandbox test binary. make sandbox-test builds the
# executor image first and passes it in.
ARG EXECUTOR_IMAGE=openplan-executor:sandbox

FROM golang:1.26.10-alpine3.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go test -c -tags sandbox -o /out/sandbox.test ./internal/sandbox/sandboxtest

FROM ${EXECUTOR_IMAGE}
COPY --from=build /out/sandbox.test /usr/local/bin/sandbox.test
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/sandbox.test", "-test.v", "-test.count=1", "-test.timeout=20m"]
