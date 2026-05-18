# Runtime API OpenAPI

## TL;DR

- The canonical machine-readable Runtime API spec lives in [`runtime-v1.yaml`](./runtime-v1.yaml).
- The operator/control-plane companion spec lives in [`operator-v1.yaml`](./operator-v1.yaml).
- Preview it locally with Swagger UI or Redoc before shipping API changes.
- When the runtime API changes, update this folder and the human-facing API references together.

English is canonical in this file. 中文同步 follows after the English section.

## Scope

The canonical OpenAPI documents in this folder are:

- [`runtime-v1.yaml`](./runtime-v1.yaml) for the stable `/runtime/*` and `/healthz` surface
- [`operator-v1.yaml`](./operator-v1.yaml) for the faster-moving `/operator/*` control-plane surface

The runtime spec is intentionally release-oriented:

- it covers the public `/runtime/*` and `/healthz` HTTP surfaces that exist today
- nested payloads that still evolve quickly remain modeled as open objects
- the goal is accurate integration guidance without overpromising every internal field

## Preview With Swagger UI

From the repository root:

```bash
docker run --rm \
  -p 18080:8080 \
  -e SWAGGER_JSON=/spec/runtime-v1.yaml \
  -v "$PWD/docs/openapi":/spec \
  swaggerapi/swagger-ui
```

Then open `http://127.0.0.1:18080`.

To preview the operator surface instead:

```bash
docker run --rm \
  -p 18080:8080 \
  -e SWAGGER_JSON=/spec/operator-v1.yaml \
  -v "$PWD/docs/openapi":/spec \
  swaggerapi/swagger-ui
```

## Preview With Redoc

If you prefer Redocly CLI:

```bash
npx @redocly/cli preview-docs docs/openapi/runtime-v1.yaml
npx @redocly/cli preview-docs docs/openapi/operator-v1.yaml
```

## Update Checklist

These documents are maintained against the live server routes and response models. When the Runtime API or operator API changes, update:

- [`docs/openapi/runtime-v1.yaml`](./runtime-v1.yaml)
- [`docs/openapi/operator-v1.yaml`](./operator-v1.yaml)
- [`docs/reference/api.md`](../reference/api.md)
- the install and API references in [`README.md`](../../README.md)
- the Chinese mirror in [`README.zh-CN.md`](../../README.zh-CN.md)

## 中文同步

### TL;DR

- 机器可读的 Runtime API 规范在 [`runtime-v1.yaml`](./runtime-v1.yaml)
- 运维/控制面规范在 [`operator-v1.yaml`](./operator-v1.yaml)
- 发布 API 变更前，先用 Swagger UI 或 Redoc 本地预览
- Runtime API 变动时，要同时更新这份规范和面向人的 API 文档

### 本地预览

```bash
docker run --rm \
  -p 18080:8080 \
  -e SWAGGER_JSON=/spec/runtime-v1.yaml \
  -v "$PWD/docs/openapi":/spec \
  swaggerapi/swagger-ui
```

或者：

```bash
npx @redocly/cli preview-docs docs/openapi/runtime-v1.yaml
npx @redocly/cli preview-docs docs/openapi/operator-v1.yaml
```

### 同步更新清单

- 更新 [`docs/openapi/runtime-v1.yaml`](./runtime-v1.yaml)
- 更新 [`docs/openapi/operator-v1.yaml`](./operator-v1.yaml)
- 更新 [`docs/reference/api.md`](../reference/api.md)
- 更新 [`README.md`](../../README.md) 里的安装和 API 入口引用
- 更新 [`README.zh-CN.md`](../../README.zh-CN.md) 的中文镜像说明
