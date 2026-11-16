# This Dockerfile is only for development environment
# Release versions use distroless images built via GoReleaser with ko
# See .goreleaser.yml
#
FROM golang:1.27.3 AS build
WORKDIR /app
RUN go install github.com/go-task/task/v3/cmd/task@v3.54.0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN $(go env GOPATH)/bin/task build

FROM alpine:3.24.3
RUN apk update && apk add --no-cache ca-certificates
WORKDIR /
COPY --from=build /app/dist/speedtest_exporter .
USER nobody
ENTRYPOINT ["/speedtest_exporter"]