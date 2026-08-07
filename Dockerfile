# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Cached separately so dependency downloads survive source-only changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/cu .

# Runtime stage
FROM debian:bookworm-slim

# fzf is no longer installed: it is compiled into the binary. What remains are
# only the tools cu shells out to.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    groff \
    less \
    openssh-client \
    && rm -rf /var/lib/apt/lists/*

# Session Manager plugin, for `cu aws ec2-ssm` (cu talks to SSM via the AWS
# SDK directly; only the interactive data-channel protocol needs the plugin).
RUN curl -fsSL "https://s3.amazonaws.com/session-manager-downloads/plugin/latest/ubuntu_64bit/session-manager-plugin.deb" \
    -o "session-manager-plugin.deb" && \
    dpkg -i session-manager-plugin.deb && \
    rm -f session-manager-plugin.deb

COPY --from=builder /out/cu /usr/local/bin/cu

ENV AWS_DEFAULT_REGION=us-east-1

RUN useradd -m -s /bin/bash cloudutil
USER cloudutil
WORKDIR /home/cloudutil

ENTRYPOINT ["cu"]
CMD ["--help"]
