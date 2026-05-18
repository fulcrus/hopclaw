---
name: docker
description: Build, run, manage, and inspect Docker containers and images
homepage: https://docs.docker.com/engine/reference/commandline/cli/
user-invocable: true
command-dispatch: tool
command-tool: docker.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: infra.docker
    emoji: "\U0001F433"
    requires:
      bins:
        - docker
    always: false
---
# Docker

Manage Docker containers, images, volumes, and networks from the CLI.

## Capabilities

- Build images from Dockerfiles
- Run, stop, start, and remove containers
- List and inspect containers and images
- View container logs and resource usage
- Execute commands inside running containers
- Manage volumes and networks
- Push and pull images from registries

## Usage

When the user asks about Docker operations, use the `docker` CLI.
Always check the Docker daemon is running before executing commands.

### Building Images

```bash
# Build from current directory
docker build -t myapp:latest .

# Build with build args
docker build --build-arg VERSION=1.2.3 -t myapp:1.2.3 .

# Multi-stage build with target
docker build --target production -t myapp:prod .
```

### Running Containers

```bash
# Run in detached mode with port mapping
docker run -d --name myapp -p 8080:80 myapp:latest

# Run with environment variables and volume mount
docker run -d --name db \
  -e POSTGRES_PASSWORD=secret \
  -v pgdata:/var/lib/postgresql/data \
  postgres:16

# Run interactively
docker run -it --rm ubuntu:22.04 bash

# Run with resource limits
docker run -d --name worker --memory=512m --cpus=1.0 worker:latest
```

### Managing Containers

```bash
# List running containers
docker ps

# List all containers including stopped
docker ps -a

# Stop and remove
docker stop myapp && docker rm myapp

# Restart a container
docker restart myapp

# View container details
docker inspect myapp --format '{{.State.Status}}'
```

### Logs and Debugging

```bash
# Follow logs
docker logs -f --tail 100 myapp

# Logs since a timestamp
docker logs --since 2024-01-01T00:00:00 myapp

# Execute a command in a running container
docker exec -it myapp sh

# View resource usage
docker stats --no-stream
```

### Image Management

```bash
# List images
docker images

# Remove unused images
docker image prune -f

# Tag and push
docker tag myapp:latest registry.example.com/myapp:latest
docker push registry.example.com/myapp:latest

# Pull an image
docker pull nginx:alpine
```

### Volumes and Networks

```bash
# List volumes
docker volume ls

# Create a network
docker network create mynet

# Connect a container to a network
docker network connect mynet myapp
```

### Docker Compose

```bash
# Start services
docker compose up -d

# Stop services
docker compose down

# View service logs
docker compose logs -f web

# Rebuild and restart
docker compose up -d --build
```

## Error Handling

- If "Cannot connect to the Docker daemon", the Docker service is not running. Advise the user to start it.
- If "port is already allocated", identify the conflicting container with `docker ps` and suggest an alternative port.
- If build fails, show the full build output and identify the failing step.

## Security

- Never expose passwords or secrets in `docker run` commands in logs. Use `--env-file` when possible.
- Warn the user before running containers with `--privileged` or `--network=host`.
- Avoid running containers as root unless necessary; suggest `--user` flag.
- Never push images to public registries without explicit user confirmation.
