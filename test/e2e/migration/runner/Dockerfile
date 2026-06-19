# Build the lightweight migration e2e runner.
FROM golang:1.26.4 AS builder
WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -o migration-runner .

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/migration-runner .
USER 65532:65532
ENTRYPOINT ["/migration-runner"]
