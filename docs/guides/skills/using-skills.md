# Using Skills

## TL;DR

- Discover installed skills with `hopclaw skills list`
- Search the catalog with `hopclaw skills search <query>`
- Install from the catalog or a local directory with `hopclaw skills install`

English is canonical in this file. 中文同步 follows after the English section.

## Inspect What Is Already Available

```bash
hopclaw skills list
hopclaw skills info summarize
```

If you want machine-readable output:

```bash
hopclaw --json skills list
```

## Search And Install

Search the catalog:

```bash
hopclaw skills search summarize
```

Install a catalog skill:

```bash
hopclaw skills install summarize
```

Install from a local directory:

```bash
hopclaw skills install ./skills/my-skill
```

## Remove A Skill

```bash
hopclaw skills remove summarize
```

## Choose An Install Policy

These settings control what happens when an agent needs a missing skill at runtime:

```yaml
skills:
  install_policy: ask
```

Available policies:

- `ask`: create an approval and wait for confirmation
- `auto`: install automatically and continue
- `deny`: block runtime installation

## Troubleshoot Missing Skill Dependencies

If a skill is discovered but not ready:

```bash
hopclaw doctor skills
hopclaw skills info summarize
```

## 中文同步

### TL;DR

- 先用 `hopclaw skills list` 看本机已发现的技能
- 用 `hopclaw skills search <query>` 搜目录
- 用 `hopclaw skills install` 从目录或本地路径安装

### 常用命令

- 查看详情：`hopclaw skills info <name>`
- 安装目录技能：`hopclaw skills install ./skills/my-skill`
- 删除技能：`hopclaw skills remove <name>`
- 排障：`hopclaw doctor skills`

