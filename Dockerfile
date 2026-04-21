# Build stage
FROM golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y gcc libc6-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go build -o /cymbal .

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates git && rm -rf /var/lib/apt/lists/*

COPY --from=builder /cymbal /usr/local/bin/cymbal

WORKDIR /workspace

# Default DB path — stored as a dotfile inside the mounted repo.
# Override with -e CYMBAL_DB=... or --db flag.
ENV CYMBAL_DB=/workspace/.cymbal/index.db
ENV CYMBAL_DOCKER_IMAGE=1

ENTRYPOINT ["cymbal"]
