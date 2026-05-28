# Build stage
FROM python:3.13-slim as builder

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Install UV
RUN curl -LsSf https://astral.sh/uv/install.sh | sh
ENV PATH="/root/.cargo/bin:${PATH}"

# Set working directory
WORKDIR /app

# Copy project files
COPY . .

# Build the package
RUN uv build

# Runtime stage
FROM python:3.13-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    fzf \
    groff \
    less \
    curl \
    unzip \
    git \
    openssh-client \
    && rm -rf /var/lib/apt/lists/*

# Install AWS CLI v2
RUN curl "https://awscli.amazonaws.com/awscliv2.zip" -o "awscliv2.zip" && \
    unzip awscliv2.zip && \
    ./aws/install && \
    rm -rf awscliv2.zip aws

# Install Session Manager Plugin
RUN curl "https://s3.amazonaws.com/session-manager-downloads/plugin/latest/ubuntu_64bit/session-manager-plugin.deb" -o "session-manager-plugin.deb" && \
    apt-get update && dpkg -i session-manager-plugin.deb && \
    rm -f session-manager-plugin.deb && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

# Install UV for runtime
RUN curl -LsSf https://astral.sh/uv/install.sh | sh
ENV PATH="/root/.cargo/bin:${PATH}"

# Set working directory
WORKDIR /app

# Copy built package from builder
COPY --from=builder /app/dist /app/dist

# Install the built package
RUN pip install --no-cache-dir /app/dist/*.whl

# Set environment variables for AWS
ENV AWS_DEFAULT_REGION=us-east-1

# Create a non-root user (optional but recommended)
RUN useradd -m -s /bin/bash cloudutil
USER cloudutil

# Set entrypoint to the CLI command
ENTRYPOINT ["cu"]
CMD ["--help"]
