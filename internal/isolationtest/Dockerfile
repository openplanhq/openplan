# The executor image plus the isolation test binary. make isolation-test
# builds the executor image first and passes it in.
ARG EXECUTOR_IMAGE=openplan-executor:isolation

FROM golang:1.25.14-alpine3.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go test -c -tags isolation -o /out/isolation.test ./internal/isolationtest

FROM ${EXECUTOR_IMAGE}
COPY --from=build /out/isolation.test /usr/local/bin/isolation.test
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/isolation.test", "-test.v", "-test.count=1"]
