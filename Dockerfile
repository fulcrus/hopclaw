FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown
ARG CHANNEL=stable

RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w \
      -X github.com/fulcrus/hopclaw/internal/version.Version=${VERSION} \
      -X github.com/fulcrus/hopclaw/internal/version.Channel=${CHANNEL} \
      -X github.com/fulcrus/hopclaw/internal/version.GitCommit=${GIT_COMMIT} \
      -X github.com/fulcrus/hopclaw/internal/version.BuildDate=${BUILD_DATE}" \
    -o /out/hopclaw ./cmd/hopclaw

FROM alpine:3.22 AS runtime

RUN apk add --no-cache ca-certificates && \
    addgroup -S hopclaw && \
    adduser -S -G hopclaw -h /home/hopclaw hopclaw && \
    mkdir -p \
      /home/hopclaw/.hopclaw/state/sessions \
      /home/hopclaw/.hopclaw/state/runs \
      /home/hopclaw/.hopclaw/state/approvals \
      /home/hopclaw/.hopclaw/artifacts \
      /home/hopclaw/.hopclaw/audit \
      /home/hopclaw/.hopclaw/skills \
      /home/hopclaw/.hopclaw/plugins/.disabled \
      /home/hopclaw/.hopclaw/clawhub/index \
      /home/hopclaw/.hopclaw/clawhub/cache \
      /home/hopclaw/.hopclaw/clawhub/bundles \
      /home/hopclaw/.hopclaw/clawhub/installs \
      /home/hopclaw/.hopclaw/clawhub/locks \
      /home/hopclaw/.hopclaw/logs \
      /home/hopclaw/.hopclaw/data \
      /home/hopclaw/.hopclaw/settings \
      /home/hopclaw/.hopclaw/device-pairing \
      /home/hopclaw/.hopclaw/workspace/canvas \
      /etc/hopclaw && \
    chown -R hopclaw:hopclaw /home/hopclaw /etc/hopclaw

COPY --from=builder /out/hopclaw /usr/local/bin/hopclaw

ENV HOME=/home/hopclaw

VOLUME ["/home/hopclaw/.hopclaw"]

EXPOSE 16280

USER hopclaw
WORKDIR /home/hopclaw

ENTRYPOINT ["hopclaw", "serve"]
