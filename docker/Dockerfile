# ============================================================
# Stage 1: Build the picoclaw binary
# ============================================================
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN make build

# ============================================================
# Stage 2: Minimal runtime image
# ============================================================
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata curl sudo

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q --spider http://localhost:18790/health || exit 1

# Copy binary
COPY --from=builder /src/build/picoclaw /usr/local/bin/picoclaw

# Create non-root user and group
RUN addgroup -g 1000 picoclaw && \
    adduser -D -u 1000 -G picoclaw picoclaw && \
    mkdir -p /etc/sudoers.d && \
    printf '%s\n' \
    'Defaults:picoclaw !requiretty' \
    'Cmnd_Alias PICOCLAW_PKG = /sbin/apk add *, /sbin/apk update, /sbin/apk upgrade, /sbin/apk info *, /sbin/apk search *, /bin/apk add *, /bin/apk update, /bin/apk upgrade, /bin/apk info *, /bin/apk search *, /usr/bin/apt update, /usr/bin/apt install *, /usr/bin/apt-get update, /usr/bin/apt-get install *, /usr/bin/yum makecache, /usr/bin/yum install *, /usr/bin/dnf makecache, /usr/bin/dnf install *' \
    'picoclaw ALL=(root) NOPASSWD: PICOCLAW_PKG' \
    > /etc/sudoers.d/picoclaw && \
    chmod 0440 /etc/sudoers.d/picoclaw

# Switch to non-root user
USER picoclaw

# Run onboard to create initial directories and config
RUN /usr/local/bin/picoclaw onboard

ENTRYPOINT ["picoclaw"]
CMD ["gateway"]
