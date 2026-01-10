FROM golang:1.25.5 AS build
WORKDIR /app
RUN curl -sL https://taskfile.dev/install.sh | sh
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN /app/bin/task build

FROM alpine:3.23.2
RUN apk update && apk add --no-cache ca-certificates
WORKDIR /
COPY --from=build /app/dist/speedtest_exporter .
USER nobody
ENTRYPOINT ["/speedtest_exporter"]