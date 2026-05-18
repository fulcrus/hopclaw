# Feishu Suite Bundle

`feishu-suite` is Hopclaw's first product-style bundle for Feishu/Lark.

It is intentionally structured as the reference implementation for future external product integrations:

- `BUNDLE.yaml` is the source of truth for discovery and tool registration.
- `runtime/feishu_suite.py` implements the standard executable bundle protocol.
- Tool names follow the product resource verb convention, for example `feishu.doc.read`.

Current tool surface:

- Docx: read, create, write, list blocks
- Wiki: list spaces, list/get/create/move/rename nodes
- Drive: list files, create folders, move/delete files, list/add/remove permissions
- Bitable: metadata, fields, records list/get/create/update
- URL resolver: normalize Feishu/Lark URLs into product tokens

Required runtime environment:

- `FEISHU_APP_ID`
- `FEISHU_APP_SECRET`
- optional `FEISHU_DOMAIN`

The runtime returns standard Hopclaw bundle protocol envelopes with:

- normalized status
- structured errors
- verification blocks for write operations when readback is possible
