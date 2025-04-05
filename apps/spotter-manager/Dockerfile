# apps/spotter-manager/Dockerfile

FROM golang:1.24 AS builder

WORKDIR /tmp/build
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o spotter-manager cmd/spotter-manager/*.go

FROM scratch AS runner
WORKDIR /app
COPY --from=builder /tmp/build/spotter-manager /app/spotter-manager
COPY --from=builder /tmp/build/web /app/web
ENTRYPOINT ["/app/spotter-manager"]
