#!/usr/bin/env python3

import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
from typing import Any, Dict, List, Optional, Tuple


PROTOCOL_VERSION = "hopclaw.tool/v1"

DOCX_URL_RE = re.compile(r"/docx/([A-Za-z0-9]+)")
WIKI_URL_RE = re.compile(r"/wiki/([A-Za-z0-9]+)")
BITABLE_URL_RE = re.compile(r"/base/([A-Za-z0-9]+)")


class ToolError(Exception):
    def __init__(
        self,
        message: str,
        *,
        code: str = "tool_error",
        category: str = "internal",
        retryable: bool = False,
        details: Optional[Dict[str, Any]] = None,
    ) -> None:
        super().__init__(message)
        self.code = code
        self.category = category
        self.retryable = retryable
        self.details = details or {}


def json_dump(data: Dict[str, Any]) -> None:
    sys.stdout.write(json.dumps(data, ensure_ascii=False))
    sys.stdout.flush()


def success(
    *,
    summary: str,
    data: Any = None,
    evidence: Optional[List[Dict[str, Any]]] = None,
    verification: Optional[Dict[str, Any]] = None,
    status: str = "success",
) -> Dict[str, Any]:
    payload: Dict[str, Any] = {
        "protocol_version": PROTOCOL_VERSION,
        "status": status,
        "summary": summary,
    }
    if data is not None:
        payload["data"] = data
    if evidence:
        payload["evidence"] = evidence
    if verification:
        payload["verification"] = verification
    return payload


def failure(err: ToolError) -> Dict[str, Any]:
    return {
        "protocol_version": PROTOCOL_VERSION,
        "status": "retryable_error" if err.retryable else "error",
        "summary": str(err),
        "error": {
            "code": err.code,
            "category": err.category,
            "message": str(err),
            "retryable": err.retryable,
            "details": err.details or None,
        },
    }


def product_base_url() -> str:
    raw = (os.environ.get("FEISHU_DOMAIN") or "").strip()
    if not raw:
        return "https://feishu.cn"
    if "://" not in raw:
        raw = "https://" + raw
    parsed = urllib.parse.urlparse(raw)
    host = parsed.netloc or parsed.path
    if host.startswith("open."):
        host = host[5:]
    if not host:
        host = "feishu.cn"
    scheme = parsed.scheme or "https"
    return f"{scheme}://{host}"


def api_base_url() -> str:
    raw = (os.environ.get("FEISHU_DOMAIN") or "").strip()
    if not raw:
        return "https://open.feishu.cn"
    if "://" not in raw:
        raw = "https://" + raw
    parsed = urllib.parse.urlparse(raw)
    host = parsed.netloc or parsed.path
    if not host:
        host = "open.feishu.cn"
    if not host.startswith("open."):
        if host.endswith("larksuite.com"):
            host = "open.larksuite.com"
        else:
            host = "open.feishu.cn"
    scheme = parsed.scheme or "https"
    return f"{scheme}://{host}"


def normalize_space(text: str) -> str:
    return " ".join((text or "").split())


def normalize_markdown_text(text: str) -> str:
    value = text or ""
    value = re.sub(r"^[#>\-\*\+\s]+", "", value, flags=re.MULTILINE)
    value = re.sub(r"`{1,3}", "", value)
    value = re.sub(r"\[(.*?)\]\((.*?)\)", r"\1", value)
    return normalize_space(value)


def require_string(input_data: Dict[str, Any], key: str) -> str:
    value = input_data.get(key)
    if not isinstance(value, str) or not value.strip():
        raise ToolError(
            f"missing required string field: {key}",
            code="missing_field",
            category="validation",
            details={"field": key},
        )
    return value.strip()


def optional_string(input_data: Dict[str, Any], key: str) -> str:
    value = input_data.get(key)
    return value.strip() if isinstance(value, str) else ""


def optional_int(input_data: Dict[str, Any], key: str, default: int) -> int:
    value = input_data.get(key)
    if value is None:
        return default
    if isinstance(value, bool):
        raise ToolError(
            f"field {key} must be an integer",
            code="invalid_field",
            category="validation",
            details={"field": key},
        )
    if isinstance(value, int):
        return value
    raise ToolError(
        f"field {key} must be an integer",
        code="invalid_field",
        category="validation",
        details={"field": key},
    )


def optional_object(input_data: Dict[str, Any], key: str) -> Dict[str, Any]:
    value = input_data.get(key)
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ToolError(
            f"field {key} must be an object",
            code="invalid_field",
            category="validation",
            details={"field": key},
        )
    return value


def ensure_auth_env() -> Tuple[str, str]:
    app_id = (os.environ.get("FEISHU_APP_ID") or "").strip()
    app_secret = (os.environ.get("FEISHU_APP_SECRET") or "").strip()
    if not app_id or not app_secret:
        missing: List[str] = []
        if not app_id:
            missing.append("FEISHU_APP_ID")
        if not app_secret:
            missing.append("FEISHU_APP_SECRET")
        raise ToolError(
            "missing Feishu credentials",
            code="missing_credentials",
            category="auth",
            details={"missing_env": missing},
        )
    return app_id, app_secret


def http_json(
    method: str,
    path: str,
    *,
    token: Optional[str] = None,
    query: Optional[Dict[str, Any]] = None,
    body: Optional[Any] = None,
    extra_headers: Optional[Dict[str, str]] = None,
) -> Dict[str, Any]:
    url = api_base_url() + path
    params = {}
    if query:
        for key, value in query.items():
            if value is None or value == "":
                continue
            params[key] = value
    if params:
        url += "?" + urllib.parse.urlencode(params, doseq=True)

    headers = {"Content-Type": "application/json; charset=utf-8"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    if extra_headers:
        headers.update(extra_headers)

    data = None
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")

    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read()
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            payload = json.loads(raw.decode("utf-8"))
        except Exception:
            payload = {}
        message = payload.get("msg") or payload.get("message") or f"HTTP {e.code}"
        category = "upstream"
        if e.code == 401:
            category = "auth"
        elif e.code == 403:
            category = "permission"
        elif e.code == 404:
            category = "not_found"
        elif e.code == 409:
            category = "conflict"
        elif e.code == 429:
            category = "rate_limit"
        elif e.code in (502, 503, 504):
            category = "network"
        raise ToolError(
            message,
            code="http_error",
            category=category,
            retryable=e.code in (429, 502, 503, 504),
            details={"http_status": e.code, "path": path, "payload": payload or None},
        )
    except urllib.error.URLError as e:
        raise ToolError(
            f"network error: {e.reason}",
            code="network_error",
            category="network",
            retryable=True,
            details={"path": path},
        )

    try:
        return json.loads(raw.decode("utf-8"))
    except Exception as e:
        raise ToolError(
            f"invalid upstream response: {e}",
            code="invalid_response",
            category="upstream",
            retryable=True,
            details={"path": path},
        )


def classify_api_error(message: str) -> Tuple[str, str]:
    text = (message or "").lower()
    if "auth" in text or "token" in text or "app secret" in text:
        return "auth", "auth_failed"
    if "permission" in text or "forbidden" in text or "no access" in text:
        return "permission", "permission_denied"
    if "not found" in text or "does not exist" in text:
        return "not_found", "resource_not_found"
    if "rate" in text or "quota" in text:
        return "rate_limit", "rate_limited"
    return "upstream", "api_error"


def api_call(
    method: str,
    path: str,
    *,
    query: Optional[Dict[str, Any]] = None,
    body: Optional[Any] = None,
    token: Optional[str] = None,
) -> Dict[str, Any]:
    payload = http_json(method, path, token=token, query=query, body=body)
    code = payload.get("code", 0)
    if code not in (0, None):
        message = str(payload.get("msg") or "Feishu API error")
        category, err_code = classify_api_error(message)
        raise ToolError(
            message,
            code=err_code,
            category=category,
            retryable=category in ("rate_limit", "network"),
            details={"path": path, "api_code": code},
        )
    data = payload.get("data")
    return data if isinstance(data, dict) else {}


def get_tenant_access_token() -> str:
    app_id, app_secret = ensure_auth_env()
    payload = http_json(
        "POST",
        "/open-apis/auth/v3/tenant_access_token/internal",
        body={"app_id": app_id, "app_secret": app_secret},
    )
    token = payload.get("tenant_access_token") or payload.get("app_access_token")
    if isinstance(token, str) and token.strip():
        return token.strip()
    message = str(payload.get("msg") or "failed to get tenant access token")
    category, err_code = classify_api_error(message)
    raise ToolError(
        message,
        code=err_code,
        category=category,
        details={"payload": payload},
    )


def parse_url(url: str) -> Optional[Dict[str, Any]]:
    try:
        parsed = urllib.parse.urlparse(url)
    except Exception:
        return None
    if not parsed.scheme or not parsed.netloc:
        return None

    table_id = urllib.parse.parse_qs(parsed.query).get("table", [None])[0]
    for regex, kind, field in (
        (DOCX_URL_RE, "docx", "document_id"),
        (WIKI_URL_RE, "wiki", "node_token"),
        (BITABLE_URL_RE, "bitable", "app_token"),
    ):
        match = regex.search(parsed.path or "")
        if match:
            out = {"kind": kind, field: match.group(1), "url": url}
            if table_id:
                out["table_id"] = table_id
            return out
    return None


def resolve_url_detail(url: str, token: Optional[str]) -> Dict[str, Any]:
    parsed = parse_url(url)
    if not parsed:
        raise ToolError(
            "unsupported Feishu URL",
            code="unsupported_url",
            category="validation",
            details={"url": url},
        )
    if parsed["kind"] != "wiki":
        return parsed
    if not token:
        parsed["requires_auth_to_resolve"] = True
        return parsed

    node = api_call(
        "GET",
        "/open-apis/wiki/v2/spaces/get_node",
        query={"token": parsed["node_token"]},
        token=token,
    ).get("node", {})
    result = {
        "kind": "wiki",
        "url": url,
        "node_token": node.get("node_token") or parsed["node_token"],
        "space_id": node.get("space_id"),
        "title": node.get("title"),
        "obj_type": node.get("obj_type"),
        "obj_token": node.get("obj_token"),
    }
    if result["obj_type"] == "docx":
        result["kind"] = "docx"
        result["document_id"] = result["obj_token"]
    elif result["obj_type"] == "bitable":
        result["kind"] = "bitable"
        result["app_token"] = result["obj_token"]
        if parsed.get("table_id"):
            result["table_id"] = parsed["table_id"]
    return result


def ensure_document_id(input_data: Dict[str, Any], token: str) -> str:
    document_id = optional_string(input_data, "document_id")
    if document_id:
        return document_id
    url = optional_string(input_data, "url")
    if not url:
        raise ToolError(
            "document_id or url is required",
            code="missing_document_id",
            category="validation",
        )
    resolved = resolve_url_detail(url, token)
    if resolved.get("kind") != "docx" or not resolved.get("document_id"):
        raise ToolError(
            "url does not resolve to a docx document",
            code="invalid_document_url",
            category="validation",
            details={"resolved": resolved},
        )
    return str(resolved["document_id"])


def ensure_node_token(input_data: Dict[str, Any]) -> str:
    token = optional_string(input_data, "node_token")
    if token:
        return token
    url = optional_string(input_data, "url")
    if not url:
        raise ToolError(
            "node_token or url is required",
            code="missing_node_token",
            category="validation",
        )
    resolved = parse_url(url)
    if not resolved or resolved.get("kind") != "wiki":
        raise ToolError(
            "url does not point to a wiki node",
            code="invalid_wiki_url",
            category="validation",
        )
    return str(resolved["node_token"])


def ensure_bitable_target(input_data: Dict[str, Any], token: str) -> Tuple[str, str]:
    app_token = optional_string(input_data, "app_token")
    table_id = optional_string(input_data, "table_id")
    if app_token and table_id:
        return app_token, table_id
    url = optional_string(input_data, "url")
    if url:
        resolved = resolve_url_detail(url, token)
        if resolved.get("kind") == "bitable" and resolved.get("app_token"):
            app_token = str(resolved["app_token"])
            if not table_id and resolved.get("table_id"):
                table_id = str(resolved["table_id"])
    if not app_token or not table_id:
        raise ToolError(
            "app_token and table_id are required",
            code="missing_bitable_target",
            category="validation",
            details={"app_token": bool(app_token), "table_id": bool(table_id)},
        )
    return app_token, table_id


def clean_descendant_blocks(blocks: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    cleaned: List[Dict[str, Any]] = []
    for block in blocks:
        item = dict(block)
        item.pop("parent_id", None)
        if item.get("block_type") == 32 and isinstance(item.get("children"), str):
            item["children"] = [item["children"]]
        if item.get("block_type") == 31 and isinstance(item.get("table"), dict):
            table = dict(item["table"])
            table.pop("cells", None)
            prop = dict(table.get("property") or {})
            allowed_prop = {}
            for key in ("row_size", "column_size", "column_width"):
                if key in prop:
                    allowed_prop[key] = prop[key]
            table = {"property": allowed_prop}
            item["table"] = table
        cleaned.append(item)
    return cleaned


def split_markdown_by_headings(markdown: str) -> List[str]:
    if len(markdown) <= 20000:
        return [markdown]
    lines = markdown.split("\n")
    chunks: List[str] = []
    current: List[str] = []
    in_fence = False
    for line in lines:
        if re.match(r"^(`{3,}|~{3,})", line):
            in_fence = not in_fence
        if not in_fence and re.match(r"^#{1,2}\s", line) and current:
            chunks.append("\n".join(current))
            current = []
        current.append(line)
    if current:
        chunks.append("\n".join(current))
    return chunks if len(chunks) > 1 else [markdown]


def split_markdown_by_size(markdown: str, max_chars: int) -> List[str]:
    if len(markdown) <= max_chars:
        return [markdown]
    lines = markdown.split("\n")
    chunks: List[str] = []
    current: List[str] = []
    current_len = 0
    in_fence = False
    for line in lines:
        if re.match(r"^(`{3,}|~{3,})", line):
            in_fence = not in_fence
        line_len = len(line) + 1
        if current and current_len + line_len > max_chars and not in_fence:
            chunks.append("\n".join(current))
            current = []
            current_len = 0
        current.append(line)
        current_len += line_len
    if current:
        chunks.append("\n".join(current))
    return chunks if len(chunks) > 1 else [markdown]


def convert_markdown(token: str, markdown: str) -> Tuple[List[Dict[str, Any]], List[str]]:
    data = api_call(
        "POST",
        "/open-apis/docx/v1/documents/blocks/convert",
        body={"content_type": "markdown", "content": markdown},
        token=token,
    )
    blocks = data.get("blocks") or []
    first_level = data.get("first_level_block_ids") or []
    return list(blocks), list(first_level)


def convert_markdown_recursive(token: str, markdown: str, depth: int = 0) -> Tuple[List[Dict[str, Any]], List[str]]:
    try:
        return convert_markdown(token, markdown)
    except ToolError:
        if depth >= 6 or len(markdown) < 512:
            raise
        chunks = split_markdown_by_size(markdown, max(256, len(markdown) // 2))
        if len(chunks) <= 1:
            raise
        blocks: List[Dict[str, Any]] = []
        first_level: List[str] = []
        for chunk in chunks:
            part_blocks, part_ids = convert_markdown_recursive(token, chunk, depth + 1)
            blocks.extend(part_blocks)
            first_level.extend(part_ids)
        return blocks, first_level


def chunked_convert_markdown(token: str, markdown: str) -> Tuple[List[Dict[str, Any]], List[str]]:
    blocks: List[Dict[str, Any]] = []
    first_level: List[str] = []
    for chunk in split_markdown_by_headings(markdown):
        part_blocks, part_ids = convert_markdown_recursive(token, chunk)
        blocks.extend(part_blocks)
        first_level.extend(part_ids)
    return blocks, first_level


def list_document_blocks(token: str, document_id: str) -> List[Dict[str, Any]]:
    items: List[Dict[str, Any]] = []
    page_token = ""
    while True:
        data = api_call(
            "GET",
            f"/open-apis/docx/v1/documents/{document_id}/blocks",
            query={"page_size": 500, "page_token": page_token},
            token=token,
        )
        batch = data.get("items") or []
        items.extend(batch)
        if not data.get("has_more"):
            break
        page_token = str(data.get("page_token") or "")
        if not page_token:
            break
    return items


def clear_document_content(token: str, document_id: str) -> int:
    blocks = list_document_blocks(token, document_id)
    child_count = 0
    for block in blocks:
        if block.get("parent_id") == document_id and block.get("block_type") != 1:
            child_count += 1
    if child_count:
        api_call(
            "DELETE",
            f"/open-apis/docx/v1/documents/{document_id}/blocks/{document_id}/children/batch_delete",
            token=token,
            body={"start_index": 0, "end_index": child_count},
        )
    return child_count


def doc_read(token: str, document_id: str) -> Dict[str, Any]:
    doc_info = api_call("GET", f"/open-apis/docx/v1/documents/{document_id}", token=token).get("document", {})
    raw = api_call("GET", f"/open-apis/docx/v1/documents/{document_id}/raw_content", token=token)
    content = raw.get("content") or ""
    return {
        "document_id": document_id,
        "title": doc_info.get("title"),
        "revision_id": doc_info.get("revision_id"),
        "content": content,
        "url": f"{product_base_url()}/docx/{document_id}",
    }


def doc_write(token: str, document_id: str, content: str, mode: str, parent_block_id: str, index: int) -> Dict[str, Any]:
    if mode not in ("replace", "append"):
        raise ToolError(
            "mode must be replace or append",
            code="invalid_mode",
            category="validation",
        )
    deleted = 0
    if mode == "replace" and parent_block_id == document_id:
        deleted = clear_document_content(token, document_id)
    blocks, first_level = chunked_convert_markdown(token, content)
    inserted = 0
    if blocks:
        data = api_call(
            "POST",
            f"/open-apis/docx/v1/documents/{document_id}/blocks/{parent_block_id}/descendant",
            token=token,
            body={
                "children_id": first_level,
                "descendants": clean_descendant_blocks(blocks),
                "index": index,
            },
        )
        inserted = len(data.get("children") or []) or len(first_level)
    observed = doc_read(token, document_id)
    normalized_written = normalize_markdown_text(content)
    normalized_observed = normalize_markdown_text(str(observed.get("content") or ""))
    snippet = normalized_written[:64]
    verification_status = "passed"
    if normalized_written:
        if snippet and snippet not in normalized_observed:
            verification_status = "unknown"
    else:
        if normalized_observed:
            verification_status = "unknown"
    return {
        "document_id": document_id,
        "mode": mode,
        "blocks_deleted": deleted,
        "blocks_inserted": inserted,
        "observed_revision_id": observed.get("revision_id"),
        "observed_title": observed.get("title"),
        "observed_content_length": len(str(observed.get("content") or "")),
        "verification": {
            "attempted": True,
            "status": verification_status,
            "strategy": "post_write_raw_content_readback",
            "observed": {
                "document_id": document_id,
                "revision_id": observed.get("revision_id"),
                "content_length": len(str(observed.get("content") or "")),
                "content_contains_prefix": bool(snippet and snippet in normalized_observed),
            },
        },
    }


def bitable_meta(token: str, input_data: Dict[str, Any]) -> Dict[str, Any]:
    app_token = optional_string(input_data, "app_token")
    table_id = optional_string(input_data, "table_id")
    url = optional_string(input_data, "url")
    resolved = None
    if url:
        resolved = resolve_url_detail(url, token)
        if not app_token and resolved.get("app_token"):
            app_token = str(resolved["app_token"])
        if not table_id and resolved.get("table_id"):
            table_id = str(resolved["table_id"])
    if not app_token:
        raise ToolError(
            "app_token or bitable url is required",
            code="missing_app_token",
            category="validation",
        )
    app = api_call("GET", f"/open-apis/bitable/v1/apps/{app_token}", token=token).get("app", {})
    tables_data = api_call("GET", f"/open-apis/bitable/v1/apps/{app_token}/tables", token=token)
    tables = tables_data.get("items") or []
    return {
        "app_token": app_token,
        "table_id": table_id or None,
        "name": app.get("name"),
        "revision": app.get("revision"),
        "tables": [
            {
                "table_id": item.get("table_id"),
                "name": item.get("name"),
                "revision": item.get("revision"),
            }
            for item in tables
        ],
        "resolved_from_url": resolved,
    }


def tool_url_resolve(input_data: Dict[str, Any]) -> Dict[str, Any]:
    url = require_string(input_data, "url")
    token = None
    if (os.environ.get("FEISHU_APP_ID") or "").strip() and (os.environ.get("FEISHU_APP_SECRET") or "").strip():
        token = get_tenant_access_token()
    resolved = resolve_url_detail(url, token)
    return success(
        summary=f"Resolved Feishu URL as {resolved.get('kind')}",
        data=resolved,
        evidence=[{"kind": "url", "name": "input", "detail": url}],
    )


def tool_doc_read(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    document_id = ensure_document_id(input_data, token)
    data = doc_read(token, document_id)
    return success(
        summary=f"Read Feishu document {document_id}",
        data=data,
        evidence=[{"kind": "document", "name": document_id, "detail": data.get("title") or ""}],
    )


def tool_doc_blocks_list(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    document_id = ensure_document_id(input_data, token)
    blocks = list_document_blocks(token, document_id)
    return success(
        summary=f"Listed {len(blocks)} blocks from Feishu document {document_id}",
        data={"document_id": document_id, "items": blocks, "count": len(blocks)},
        evidence=[{"kind": "document", "name": document_id, "detail": f"blocks={len(blocks)}"}],
    )


def tool_doc_create(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    title = require_string(input_data, "title")
    folder_token = optional_string(input_data, "folder_token")
    initial_content = input_data.get("initial_content")
    if initial_content is not None and not isinstance(initial_content, str):
        raise ToolError(
            "initial_content must be a string",
            code="invalid_field",
            category="validation",
            details={"field": "initial_content"},
        )
    document = api_call(
        "POST",
        "/open-apis/docx/v1/documents",
        token=token,
        body={"title": title, "folder_token": folder_token or None},
    ).get("document", {})
    document_id = str(document.get("document_id") or "")
    if not document_id:
        raise ToolError(
            "document creation did not return document_id",
            code="missing_document_id",
            category="upstream",
        )
    verification = {
        "attempted": True,
        "status": "passed",
        "strategy": "post_create_get_document",
        "observed": {"document_id": document_id, "title": document.get("title")},
    }
    result: Dict[str, Any] = {
        "document_id": document_id,
        "title": document.get("title"),
        "url": f"{product_base_url()}/docx/{document_id}",
    }
    if initial_content:
        write_result = doc_write(token, document_id, initial_content, "replace", document_id, -1)
        verification = write_result["verification"]
        result["seed_write"] = {k: v for k, v in write_result.items() if k != "verification"}
    return success(
        summary=f"Created Feishu document {document_id}",
        data=result,
        verification=verification,
        evidence=[{"kind": "document", "name": document_id, "detail": title}],
    )


def tool_doc_write(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    document_id = ensure_document_id(input_data, token)
    content = require_string(input_data, "content")
    mode = optional_string(input_data, "mode") or "replace"
    parent_block_id = optional_string(input_data, "parent_block_id") or document_id
    index = optional_int(input_data, "index", -1)
    result = doc_write(token, document_id, content, mode, parent_block_id, index)
    verification = result.pop("verification")
    return success(
        summary=f"Wrote Feishu document {document_id} in {mode} mode",
        data=result,
        verification=verification,
        evidence=[{"kind": "document", "name": document_id, "detail": f"mode={mode}"}],
    )


def tool_wiki_space_list(input_data: Dict[str, Any]) -> Dict[str, Any]:
    _ = input_data
    token = get_tenant_access_token()
    spaces = api_call("GET", "/open-apis/wiki/v2/spaces", token=token).get("items") or []
    items = [
        {
            "space_id": item.get("space_id"),
            "name": item.get("name"),
            "description": item.get("description"),
            "visibility": item.get("visibility"),
        }
        for item in spaces
    ]
    return success(summary=f"Listed {len(items)} wiki spaces", data={"spaces": items, "count": len(items)})


def tool_wiki_node_list(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    space_id = require_string(input_data, "space_id")
    parent_node_token = optional_string(input_data, "parent_node_token")
    data = api_call(
        "GET",
        f"/open-apis/wiki/v2/spaces/{space_id}/nodes",
        query={"parent_node_token": parent_node_token},
        token=token,
    )
    items = [
        {
            "node_token": item.get("node_token"),
            "obj_token": item.get("obj_token"),
            "obj_type": item.get("obj_type"),
            "title": item.get("title"),
            "has_child": item.get("has_child"),
        }
        for item in (data.get("items") or [])
    ]
    return success(summary=f"Listed {len(items)} wiki nodes", data={"space_id": space_id, "items": items})


def tool_wiki_node_get(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    node_token = ensure_node_token(input_data)
    node = api_call(
        "GET",
        "/open-apis/wiki/v2/spaces/get_node",
        query={"token": node_token},
        token=token,
    ).get("node", {})
    return success(summary=f"Read wiki node {node_token}", data=node)


def tool_wiki_node_create(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    space_id = require_string(input_data, "space_id")
    title = require_string(input_data, "title")
    obj_type = optional_string(input_data, "obj_type") or "docx"
    parent_node_token = optional_string(input_data, "parent_node_token")
    node = api_call(
        "POST",
        f"/open-apis/wiki/v2/spaces/{space_id}/nodes",
        token=token,
        body={
            "obj_type": obj_type,
            "node_type": "origin",
            "title": title,
            "parent_node_token": parent_node_token or None,
        },
    ).get("node", {})
    verification = {
        "attempted": True,
        "status": "passed" if node.get("node_token") else "unknown",
        "strategy": "create_response_contains_node",
        "observed": {"node_token": node.get("node_token"), "title": node.get("title")},
    }
    return success(summary=f"Created wiki node {node.get('node_token') or title}", data=node, verification=verification)


def tool_wiki_node_move(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    space_id = require_string(input_data, "space_id")
    node_token = require_string(input_data, "node_token")
    target_space_id = optional_string(input_data, "target_space_id") or space_id
    target_parent_token = optional_string(input_data, "target_parent_token")
    api_call(
        "POST",
        f"/open-apis/wiki/v2/spaces/{space_id}/nodes/{node_token}/move",
        token=token,
        body={"target_space_id": target_space_id, "target_parent_token": target_parent_token or None},
    )
    verification = {
        "attempted": False,
        "status": "none",
        "strategy": "move_response_only",
        "observed": {"node_token": node_token, "target_space_id": target_space_id},
    }
    return success(
        summary=f"Moved wiki node {node_token}",
        data={"node_token": node_token, "target_space_id": target_space_id, "target_parent_token": target_parent_token or None},
        verification=verification,
    )


def tool_wiki_node_rename(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    space_id = require_string(input_data, "space_id")
    node_token = require_string(input_data, "node_token")
    title = require_string(input_data, "title")
    api_call(
        "POST",
        f"/open-apis/wiki/v2/spaces/{space_id}/nodes/{node_token}/update_title",
        token=token,
        body={"title": title},
    )
    node = api_call(
        "GET",
        "/open-apis/wiki/v2/spaces/get_node",
        query={"token": node_token},
        token=token,
    ).get("node", {})
    verification = {
        "attempted": True,
        "status": "passed" if node.get("title") == title else "unknown",
        "strategy": "post_rename_get_node",
        "observed": {"node_token": node_token, "title": node.get("title")},
    }
    return success(summary=f"Renamed wiki node {node_token}", data=node, verification=verification)


def tool_drive_file_list(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    folder_token = optional_string(input_data, "folder_token")
    data = api_call(
        "GET",
        "/open-apis/drive/v1/files",
        query={"folder_token": folder_token},
        token=token,
    )
    files = [
        {
            "token": item.get("token"),
            "name": item.get("name"),
            "type": item.get("type"),
            "url": item.get("url"),
            "created_time": item.get("created_time"),
            "modified_time": item.get("modified_time"),
            "owner_id": item.get("owner_id"),
        }
        for item in (data.get("files") or [])
    ]
    return success(summary=f"Listed {len(files)} drive files", data={"files": files, "next_page_token": data.get("next_page_token")})


def tool_drive_folder_create(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    name = require_string(input_data, "name")
    folder_token = optional_string(input_data, "folder_token") or "0"
    data = api_call(
        "POST",
        "/open-apis/drive/v1/files/create_folder",
        token=token,
        body={"name": name, "folder_token": folder_token},
    )
    created_token = data.get("token")
    verification = {
        "attempted": True,
        "status": "passed" if created_token else "unknown",
        "strategy": "create_folder_response_contains_token",
        "observed": {"token": created_token, "url": data.get("url")},
    }
    return success(summary=f"Created drive folder {name}", data=data, verification=verification)


def tool_drive_file_move(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    file_token = require_string(input_data, "file_token")
    file_type = require_string(input_data, "type")
    folder_token = require_string(input_data, "folder_token")
    data = api_call(
        "POST",
        f"/open-apis/drive/v1/files/{file_token}/move",
        query={},
        token=token,
        body={"type": file_type, "folder_token": folder_token},
    )
    return success(
        summary=f"Moved drive file {file_token}",
        data={"file_token": file_token, "folder_token": folder_token, "task_id": data.get("task_id")},
        verification={"attempted": False, "status": "none", "strategy": "move_response_only", "observed": {"task_id": data.get("task_id")}},
    )


def tool_drive_file_delete(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    file_token = require_string(input_data, "file_token")
    file_type = require_string(input_data, "type")
    data = api_call(
        "DELETE",
        f"/open-apis/drive/v1/files/{file_token}",
        query={"type": file_type},
        token=token,
    )
    return success(
        summary=f"Deleted drive file {file_token}",
        data={"file_token": file_token, "task_id": data.get("task_id")},
        verification={"attempted": False, "status": "none", "strategy": "delete_response_only", "observed": {"task_id": data.get("task_id")}},
    )


def list_permissions(token: str, resource_token: str, resource_type: str) -> List[Dict[str, Any]]:
    data = api_call(
        "GET",
        f"/open-apis/drive/v2/permissions/{resource_token}/members",
        query={"type": resource_type},
        token=token,
    )
    return list(data.get("items") or [])


def tool_drive_permission_list(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    resource_token = require_string(input_data, "token")
    resource_type = require_string(input_data, "type")
    items = list_permissions(token, resource_token, resource_type)
    return success(summary=f"Listed {len(items)} permission members", data={"items": items, "count": len(items)})


def tool_drive_permission_add(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    resource_token = require_string(input_data, "token")
    resource_type = require_string(input_data, "type")
    member_type = require_string(input_data, "member_type")
    member_id = require_string(input_data, "member_id")
    perm = require_string(input_data, "perm")
    data = api_call(
        "POST",
        f"/open-apis/drive/v2/permissions/{resource_token}/members",
        query={"type": resource_type, "need_notification": "false"},
        token=token,
        body={"member_type": member_type, "member_id": member_id, "perm": perm},
    )
    members = list_permissions(token, resource_token, resource_type)
    found = any(item.get("member_id") == member_id for item in members)
    verification = {
        "attempted": True,
        "status": "passed" if found else "unknown",
        "strategy": "post_add_list_members",
        "observed": {"member_found": found, "member_id": member_id},
    }
    return success(summary=f"Added permission for member {member_id}", data={"member": data.get("member")}, verification=verification)


def tool_drive_permission_remove(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    resource_token = require_string(input_data, "token")
    resource_type = require_string(input_data, "type")
    member_type = require_string(input_data, "member_type")
    member_id = require_string(input_data, "member_id")
    api_call(
        "DELETE",
        f"/open-apis/drive/v2/permissions/{resource_token}/members/{member_id}",
        query={"type": resource_type, "member_type": member_type},
        token=token,
    )
    members = list_permissions(token, resource_token, resource_type)
    removed = not any(item.get("member_id") == member_id for item in members)
    verification = {
        "attempted": True,
        "status": "passed" if removed else "unknown",
        "strategy": "post_remove_list_members",
        "observed": {"member_removed": removed, "member_id": member_id},
    }
    return success(summary=f"Removed permission for member {member_id}", data={"member_id": member_id}, verification=verification)


def tool_bitable_meta_get(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    data = bitable_meta(token, input_data)
    return success(summary=f"Read bitable metadata for {data.get('app_token')}", data=data)


def tool_bitable_field_list(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    app_token, table_id = ensure_bitable_target(input_data, token)
    data = api_call(
        "GET",
        f"/open-apis/bitable/v1/apps/{app_token}/tables/{table_id}/fields",
        token=token,
    )
    items = data.get("items") or []
    return success(summary=f"Listed {len(items)} bitable fields", data={"items": items, "count": len(items)})


def tool_bitable_record_list(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    app_token, table_id = ensure_bitable_target(input_data, token)
    page_size = optional_int(input_data, "page_size", 100)
    page_token = optional_string(input_data, "page_token")
    data = api_call(
        "GET",
        f"/open-apis/bitable/v1/apps/{app_token}/tables/{table_id}/records",
        token=token,
        query={"page_size": page_size, "page_token": page_token},
    )
    return success(summary=f"Listed bitable records from {table_id}", data=data)


def tool_bitable_record_get(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    app_token, table_id = ensure_bitable_target(input_data, token)
    record_id = require_string(input_data, "record_id")
    data = api_call(
        "GET",
        f"/open-apis/bitable/v1/apps/{app_token}/tables/{table_id}/records/{record_id}",
        token=token,
    )
    return success(summary=f"Read bitable record {record_id}", data=data)


def tool_bitable_record_create(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    app_token, table_id = ensure_bitable_target(input_data, token)
    fields = optional_object(input_data, "fields")
    if not fields:
        raise ToolError(
            "fields must be a non-empty object",
            code="missing_fields",
            category="validation",
        )
    data = api_call(
        "POST",
        f"/open-apis/bitable/v1/apps/{app_token}/tables/{table_id}/records",
        token=token,
        body={"fields": fields},
    )
    record = data.get("record") or {}
    record_id = record.get("record_id")
    verification = {"attempted": False, "status": "none", "strategy": "create_response_only", "observed": {"record_id": record_id}}
    if record_id:
        readback = api_call(
            "GET",
            f"/open-apis/bitable/v1/apps/{app_token}/tables/{table_id}/records/{record_id}",
            token=token,
        ).get("record", {})
        verification = {
            "attempted": True,
            "status": "passed" if readback.get("record_id") == record_id else "unknown",
            "strategy": "post_create_get_record",
            "observed": {"record_id": readback.get("record_id")},
        }
    return success(summary=f"Created bitable record {record_id or ''}".strip(), data=data, verification=verification)


def tool_bitable_record_update(input_data: Dict[str, Any]) -> Dict[str, Any]:
    token = get_tenant_access_token()
    app_token, table_id = ensure_bitable_target(input_data, token)
    record_id = require_string(input_data, "record_id")
    fields = optional_object(input_data, "fields")
    if not fields:
        raise ToolError(
            "fields must be a non-empty object",
            code="missing_fields",
            category="validation",
        )
    data = api_call(
        "PUT",
        f"/open-apis/bitable/v1/apps/{app_token}/tables/{table_id}/records/{record_id}",
        token=token,
        body={"fields": fields},
    )
    readback = api_call(
        "GET",
        f"/open-apis/bitable/v1/apps/{app_token}/tables/{table_id}/records/{record_id}",
        token=token,
    ).get("record", {})
    verification = {
        "attempted": True,
        "status": "passed" if readback.get("record_id") == record_id else "unknown",
        "strategy": "post_update_get_record",
        "observed": {"record_id": readback.get("record_id")},
    }
    return success(summary=f"Updated bitable record {record_id}", data=data, verification=verification)


TOOL_HANDLERS = {
    "feishu.url.resolve": tool_url_resolve,
    "feishu.doc.read": tool_doc_read,
    "feishu.doc.blocks.list": tool_doc_blocks_list,
    "feishu.doc.create": tool_doc_create,
    "feishu.doc.write": tool_doc_write,
    "feishu.wiki.space.list": tool_wiki_space_list,
    "feishu.wiki.node.list": tool_wiki_node_list,
    "feishu.wiki.node.get": tool_wiki_node_get,
    "feishu.wiki.node.create": tool_wiki_node_create,
    "feishu.wiki.node.move": tool_wiki_node_move,
    "feishu.wiki.node.rename": tool_wiki_node_rename,
    "feishu.drive.file.list": tool_drive_file_list,
    "feishu.drive.folder.create": tool_drive_folder_create,
    "feishu.drive.file.move": tool_drive_file_move,
    "feishu.drive.file.delete": tool_drive_file_delete,
    "feishu.drive.permission.list": tool_drive_permission_list,
    "feishu.drive.permission.add": tool_drive_permission_add,
    "feishu.drive.permission.remove": tool_drive_permission_remove,
    "feishu.bitable.meta.get": tool_bitable_meta_get,
    "feishu.bitable.field.list": tool_bitable_field_list,
    "feishu.bitable.record.list": tool_bitable_record_list,
    "feishu.bitable.record.get": tool_bitable_record_get,
    "feishu.bitable.record.create": tool_bitable_record_create,
    "feishu.bitable.record.update": tool_bitable_record_update,
}


def main() -> int:
    try:
        request = json.loads(sys.stdin.read() or "{}")
        tool_name = str(request.get("tool_name") or "").strip()
        input_data = request.get("input") or {}
        if not isinstance(input_data, dict):
            raise ToolError(
                "request input must be an object",
                code="invalid_input",
                category="validation",
            )
        handler = TOOL_HANDLERS.get(tool_name)
        if handler is None:
            raise ToolError(
                f"unsupported tool: {tool_name}",
                code="unsupported_tool",
                category="validation",
                details={"tool_name": tool_name},
            )
        json_dump(handler(input_data))
        return 0
    except ToolError as err:
        json_dump(failure(err))
        return 0
    except Exception as err:
        json_dump(
            failure(
                ToolError(
                    f"unexpected error: {err}",
                    code="unexpected_error",
                    category="internal",
                )
            )
        )
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
