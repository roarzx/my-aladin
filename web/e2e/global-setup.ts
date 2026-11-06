import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'

function findRoot(start: string): string {
  let dir = path.resolve(start)
  for (;;) {
    if (fs.existsSync(path.join(dir, 'go.mod'))) return dir
    const parent = path.dirname(dir)
    if (parent === dir) throw new Error('找不到仓库根目录（go.mod）')
    dir = parent
  }
}

function run(command: string, args: string[], cwd: string, input?: string) {
  execFileSync(command, args, {
    cwd,
    input,
    stdio: [input ? 'pipe' : 'ignore', 'inherit', 'inherit'],
  })
}

export default async function globalSetup() {
  const webRoot = path.resolve(process.cwd())
  const root = findRoot(webRoot)

  // 构建 Playwright 需要的二进制（server 由 make/CI 预先构建）
  run('go', ['build', '-o', 'bin/fakeagent', './tests/fixtures/acp/fakeagent'], root)
  run('go', ['build', '-o', 'bin/aladdin', './cmd/aladdin'], root)
  run('go', ['build', '-o', 'bin/server', './cmd/server'], root)

  // fakeagent 默认不改文件；用 wrapper 开启 write_file 模式以产生提交与 PR
  const fakeAgent = path.join(root, 'bin', 'fakeagent')
  const fakeAgentWrite = path.join(root, 'bin', 'fakeagent-write')
  fs.writeFileSync(fakeAgentWrite, `#!/bin/sh\nFAKE_ACP_MODE=write_file exec ${fakeAgent} "$@"\n`, { mode: 0o755 })

  // 演示工作区（API 不提供创建工作区）
  const sql = `
INSERT INTO workspaces (id, name) VALUES
  ('aaaaaaaa-0000-0000-0000-000000000001', '阿拉丁演示')
  ON DUPLICATE KEY UPDATE name = VALUES(name);
INSERT INTO workspace_members (workspace_id, user_id, role) VALUES
  ('aaaaaaaa-0000-0000-0000-000000000001', 1, 'owner')
  ON DUPLICATE KEY UPDATE role = VALUES(role);
`
  run(
    'docker',
    ['compose', '-f', 'deploy/docker-compose.yml', 'exec', '-T', 'mysql',
      'mysql', '-uroot', '-proot', 'aladdin_test'],
    root,
    sql,
  )

  // 每次运行重建本地 bare 仓库，避免旧分支污染
  // 每次运行使用唯一仓库路径，避免 repositories 唯一键冲突
  const e2eDir = path.join(webRoot, 'e2e')
  for (const entry of fs.readdirSync(e2eDir)) {
    if (entry.startsWith('.e2e-repo-')) {
      fs.rmSync(path.join(e2eDir, entry), { recursive: true, force: true })
    }
  }
  const repoRoot = path.join(e2eDir, `.e2e-repo-${Date.now()}`)
  const bare = path.join(repoRoot, 'origin.git')
  const work = path.join(repoRoot, 'work')
  fs.mkdirSync(work, { recursive: true })

  run('git', ['init', '--bare', bare], root)
  run('git', ['symbolic-ref', 'HEAD', 'refs/heads/main'], bare)
  run('git', ['init', '-b', 'main'], work)
  run('git', ['config', 'user.email', 'e2e@example.com'], work)
  run('git', ['config', 'user.name', 'E2E'], work)
  fs.writeFileSync(path.join(work, 'README.md'), '# aladdin e2e\n')
  run('git', ['add', '.'], work)
  run('git', ['commit', '-m', 'init'], work)
  run('git', ['remote', 'add', 'origin', bare], work)
  run('git', ['push', '-u', 'origin', 'main'], work)

  fs.writeFileSync(
    path.join(webRoot, 'e2e', '.e2e-state.json'),
    JSON.stringify({
      workspaceId: 'aaaaaaaa-0000-0000-0000-000000000001',
      repoUrl: `file://${bare}`,
      fakeAgent: fakeAgentWrite,
      repoRoot: root,
    }, null, 2),
  )
}
