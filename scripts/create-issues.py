#!/usr/bin/env python3
"""为 dev_spec.md §6.2 中的每个工作包创建一个 GitHub Issue。

脚本从规格文档解析工作包，因此文档是唯一数据源；重复运行是安全的
（已存在同编号标题的 Issue 会被跳过，标签与里程碑按需创建）。

用法：
    python3 scripts/create-issues.py                # 创建 / 补齐所有 Issue
    python3 scripts/create-issues.py --dry-run      # 只解析并打印，不调用 gh
    python3 scripts/create-issues.py --stage B,C    # 只处理指定阶段
    python3 scripts/create-issues.py --spec path/to/dev_spec.md

前提：安装 gh CLI 并已 `gh auth login`；在 GitHub 仓库内运行，或用 --repo 指定。
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

# 阶段 → 里程碑；里程碑定义见 dev_spec §6.3
STAGE_MILESTONE = {
    "A": "M1", "B": "M1",
    "C": "M2",
    "D": "M3", "E": "M3",
    "F": "M4", "G": "M4",
    "H": "M5", "I": "M5",
}

MILESTONES = {
    "M1": "A+B：可构建、可迁移、WS 握手与可靠投递通过",
    "M2": "C：daemon 能连、能起 Provider、能流式回传并在断线后恢复",
    "M3": "D+E：普通模式串行闭环 + 故障注入全部通过",
    "M4": "F+G：小队审批与验收流程可用",
    "M5": "H+I：可观测、评估、E2E、部署与证据完整",
}

LABEL_COLORS = {
    "stage:A": "0e8a16", "stage:B": "0e8a16", "stage:C": "0e8a16",
    "stage:D": "1d76db", "stage:E": "1d76db",
    "stage:F": "5319e7", "stage:G": "5319e7",
    "stage:H": "fbca04", "stage:I": "fbca04",
    "v1": "0e8a16",
    "v2": "5319e7",
    "blocked": "b60205",
}

PKG_RE = re.compile(r"^\*\*([A-Z]\d+)：(.+?)\*\*", re.MULTILINE)
FIELD_RE = re.compile(r"^- (目标|文件|实现|验收|测试)：(.*)$")


def parse_packages(spec_path: Path) -> list[dict]:
    text = spec_path.read_text(encoding="utf-8")
    try:
        start = text.index("### 6.2 开发工作包")
        end = text.index("### 6.3 里程碑与进度")
    except ValueError as exc:
        raise SystemExit(f"未在 {spec_path} 中找到 §6.2 / §6.3：{exc}") from exc

    section = text[start:end]
    matches = list(PKG_RE.finditer(section))
    if not matches:
        raise SystemExit("§6.2 中没有解析到任何工作包（标题格式应为 **A1：...**）")

    packages: list[dict] = []
    for i, m in enumerate(matches):
        chunk_end = matches[i + 1].start() if i + 1 < len(matches) else len(section)
        chunk = section[m.end():chunk_end]
        fields: dict[str, str] = {}
        for line in chunk.splitlines():
            fm = FIELD_RE.match(line)
            if fm:
                fields[fm.group(1)] = fm.group(2).strip()
        missing = [k for k in ("目标", "文件", "实现", "验收", "测试") if k not in fields]
        if missing:
            raise SystemExit(f"工作包 {m.group(1)} 缺少字段：{', '.join(missing)}")
        pkg_id = m.group(1)
        stage = pkg_id[0]
        packages.append({
            "id": pkg_id,
            "title": m.group(2).strip(),
            "stage": stage,
            "milestone": STAGE_MILESTONE[stage],
            "track": "v1" if stage <= "E" else "v2",
            **fields,
        })
    return packages


def issue_title(pkg: dict) -> str:
    # 与 §6.4 的约定一致：<编号> <工作包名>
    return f"{pkg['id']} {pkg['title']}"


def issue_body(pkg: dict) -> str:
    return f"""## 目标
{pkg['目标']}

## 文件
{pkg['文件']}

## 实现
{pkg['实现']}

## 验收
{pkg['验收']}

## 测试
```
{pkg['测试']}
```

## 完成检查
- [ ] 实现完成，`make lint` 通过
- [ ] 上面的测试命令在本地通过
- [ ] 证据归档到 `docs/evidence/{pkg['id']}/`，并把路径贴到本 Issue 的最后一条评论
- [ ] 提交信息含 `refs #<本 Issue 编号>`；完成时用 `Closes #<本 Issue 编号>`

> 规格：`dev_spec.md` §6.2 {pkg['id']}。本 Issue 只跟踪进度，契约以文档为准。
"""


def gh(*args: str, check: bool = True) -> str:
    proc = subprocess.run(["gh", *args], text=True, capture_output=True)
    if check and proc.returncode != 0:
        raise SystemExit(f"gh {' '.join(args)} 失败：\n{proc.stderr.strip()}")
    return proc.stdout.strip()


def resolve_repo(explicit: str | None) -> str:
    if explicit:
        return explicit
    try:
        return gh("repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner")
    except SystemExit:
        raise SystemExit("无法确定仓库；请在 GitHub 仓库内运行，或用 --repo owner/name 指定")


def ensure_labels(repo: str, dry_run: bool) -> None:
    existing = {row["name"] for row in json.loads(gh("label", "list", "--limit", "200", "--json", "name"))}
    for name, color in LABEL_COLORS.items():
        if name in existing:
            continue
        print(f"+ label {name}")
        if not dry_run:
            gh("label", "create", name, "--color", color, "--description", "dev_spec 工作包")


def ensure_milestones(repo: str, dry_run: bool) -> None:
    raw = gh("api", f"repos/{repo}/milestones?state=all&per_page=100", "--jq", ".[].title")
    existing = set(raw.splitlines())
    for title, desc in MILESTONES.items():
        if title in existing:
            continue
        print(f"+ milestone {title}")
        if not dry_run:
            gh("api", "-X", "POST", f"repos/{repo}/milestones", "-f", f"title={title}", "-f", f"description={desc}")


def existing_titles() -> set[str]:
    rows = json.loads(gh("issue", "list", "--state", "all", "--limit", "1000", "--json", "title"))
    return {row["title"] for row in rows}


def main() -> int:
    default_spec = Path(__file__).resolve().parent.parent / "dev_spec.md"
    ap = argparse.ArgumentParser(description="为 dev_spec §6.2 的工作包创建 GitHub Issue")
    ap.add_argument("--spec", type=Path, default=default_spec)
    ap.add_argument("--repo", default=None, help="owner/name；默认取当前仓库")
    ap.add_argument("--stage", default=None, help="只处理指定阶段，逗号分隔，如 B,C")
    ap.add_argument("--dry-run", action="store_true", help="只打印，不调用写操作")
    args = ap.parse_args()

    packages = parse_packages(args.spec)
    if args.stage:
        wanted = {s.strip().upper() for s in args.stage.split(",") if s.strip()}
        packages = [p for p in packages if p["stage"] in wanted]
        if not packages:
            raise SystemExit(f"没有匹配阶段 {args.stage} 的工作包")

    by_stage: dict[str, int] = {}
    for p in packages:
        by_stage[p["stage"]] = by_stage.get(p["stage"], 0) + 1
    summary = " ".join(f"{k}:{v}" for k, v in sorted(by_stage.items()))
    print(f"解析到 {len(packages)} 个工作包（{summary}）")

    if args.dry_run:
        for p in packages:
            print(f"{issue_title(p)}  [{p['track']} {p['milestone']} stage:{p['stage']}]")
        return 0

    repo = resolve_repo(args.repo)
    print(f"仓库：{repo}")
    ensure_labels(repo, args.dry_run)
    ensure_milestones(repo, args.dry_run)

    existing = existing_titles()
    created = skipped = 0
    for p in packages:
        title = issue_title(p)
        if title in existing:
            skipped += 1
            continue
        out = gh(
            "issue", "create",
            "--title", title,
            "--body", issue_body(p),
            "--label", f"stage:{p['stage']}",
            "--label", p["track"],
            "--milestone", p["milestone"],
        )
        created += 1
        print(f"+ {title}  {out.rsplit('/', 1)[-1] if out else ''}")

    print(f"完成：新建 {created}，已存在 {skipped}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
