---
sidebar_position: 1
title: Installation
---

# Installation

This guide shows the fastest supported ways to get HopClaw running with commands that exist in the current CLI.

## Prerequisites

- **Go 1.21 or later** for local builds and `go install`
- **Git** if you want local plugin or skill development
- **Docker** only if you prefer container deployment

## Option 1: Install the CLI with Go

```bash
go install github.com/fulcrus/hopclaw/cmd/hopclaw@latest
hopclaw version
```

Then run guided onboarding:

```bash
hopclaw onboard
```

The onboarding flow can configure:

1. operator auth mode
2. model provider
3. chat channels
4. gateway settings
5. optional daemon install

For a smaller local-only flow, use:

```bash
hopclaw setup
```

## Option 2: Build and run with Docker

The repository ships a `Dockerfile` and `docker/config.example.yaml`.

Build the image:

```bash
docker build -t hopclaw .
```

Create a config from the container example:

```bash
cp docker/config.example.yaml config.yaml
```

Run it with a bind mount:

```bash
docker run --rm \
  -p 16280:16280 \
  -v "$(pwd)/config.yaml:/etc/hopclaw/config.yaml" \
  -v "$(pwd)/.hopclaw:/home/hopclaw/.hopclaw" \
  hopclaw
```

If you prefer named volumes:

```bash
docker volume create hopclaw-data
docker run --rm \
  -p 16280:16280 \
  -v "$(pwd)/config.yaml:/etc/hopclaw/config.yaml" \
  -v hopclaw-data:/home/hopclaw/.hopclaw \
  hopclaw
```

## First run checklist

After installation:

```bash
hopclaw serve
hopclaw dashboard --open
hopclaw status
```

Expected status output looks like:

```text
Gateway: 127.0.0.1:16280
Status:  healthy
Version: <your-version>
```

## Useful follow-up commands

```bash
hopclaw doctor
hopclaw config show
hopclaw dashboard --open
hopclaw health
```

## Troubleshooting

### `no config file found`

Run one of these:

```bash
hopclaw onboard
# or
hopclaw setup
```

### Provider key already exists in your environment

HopClaw can auto-detect API keys such as `OPENAI_API_KEY`. If you want a generated local config immediately:

```bash
export OPENAI_API_KEY=your-key
hopclaw serve
```

### Docker bind mount permission error

The Docker entrypoint expects the data directory to be writable by UID `10001`.

```bash
sudo chown -R 10001:10001 ./.hopclaw
```
