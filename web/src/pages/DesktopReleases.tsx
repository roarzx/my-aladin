import { useMemo, useState, type FormEvent } from 'react'

import AppShell from '../components/AppShell'
import { IconPlus, IconSearch } from '../components/icons'
import { EmptyState, StateTag, toast } from '../components/ui'
import '../styles/pages-admin.css'

/* ------------------------------------------------------------------
   版本发布
   对照 hercules-frontend /desktop/releases。
   本地 Go 后端未提供安装包发布接口，列表恒为空，不伪造发布记录。
   ------------------------------------------------------------------ */

type Platform = 'windows' | 'macos'
type Arch = 'x64' | 'arm64'
type ReleaseStatus = 'draft' | 'partial' | 'ready' | 'published' | 'withdrawn'

const PLATFORM_LABELS: Record<Platform, string> = {
  windows: 'Windows',
  macos: 'macOS',
}

const STATUS_LABELS: Record<ReleaseStatus, string> = {
  draft: '草稿',
  partial: '部分上传',
  ready: '待发布',
  published: '已发布',
  withdrawn: '已撤回',
}

const STATUS_TONES: Record<
  ReleaseStatus,
  'draft' | 'pending' | 'review' | 'done' | 'muted'
> = {
  draft: 'draft',
  partial: 'pending',
  ready: 'review',
  published: 'done',
  withdrawn: 'muted',
}

const ACCEPT_BY_PLATFORM: Record<Platform, string> = {
  windows: '.yml,.exe,.blockmap',
  macos: '.yml,.dmg,.zip,.blockmap',
}

interface ReleaseRow {
  id: string
  version: string
  platform: Platform
  arch: Arch
  status: ReleaseStatus
  notes: string
  fileCount: number
  size: number
  createdAt: string
}

interface PendingFile {
  id: string
  name: string
  size: number
}

/** 本地后端尚未提供安装包发布能力，接口接入前列表恒为空。 */
const RELEASES: ReleaseRow[] = []

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`
  return `${(value / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function fileAllowed(name: string, platform: Platform) {
  const lower = name.toLowerCase()
  return ACCEPT_BY_PLATFORM[platform]
    .split(',')
    .some((extension) => lower.endsWith(extension))
}

interface ReleaseModalProps {
  onClose: () => void
}

function ReleaseModal({ onClose }: ReleaseModalProps) {
  const [version, setVersion] = useState('')
  const [platform, setPlatform] = useState<Platform>('windows')
  const [arch, setArch] = useState<Arch>('x64')
  const [notes, setNotes] = useState('')
  const [files, setFiles] = useState<PendingFile[]>([])
  const [notice, setNotice] = useState('')

  const addFiles = (incoming: FileList | null) => {
    if (!incoming || incoming.length === 0) return
    const accepted: PendingFile[] = []
    let rejected = 0
    Array.from(incoming).forEach((file, index) => {
      if (!fileAllowed(file.name, platform)) {
        rejected += 1
        return
      }
      accepted.push({
        id: `${file.name}-${file.size}-${index}-${Date.now()}`,
        name: file.name,
        size: file.size,
      })
    })
    if (accepted.length > 0) {
      setFiles((current) => {
        const merged = [...current]
        accepted.forEach((file) => {
          if (!merged.some((item) => item.name === file.name && item.size === file.size)) {
            merged.push(file)
          }
        })
        return merged
      })
    }
    setNotice(
      rejected > 0
        ? `${rejected} 个文件类型不受支持，当前平台仅接受 ${ACCEPT_BY_PLATFORM[platform]}。`
        : '',
    )
  }

  const removeFile = (id: string) => {
    setFiles((current) => current.filter((file) => file.id !== id))
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!version.trim() || files.length === 0) {
      setNotice('请填写版本号并至少选择一个发布文件。')
      return
    }
    setNotice('本地后端尚未提供该能力，发布记录不会写入。')
    toast('本地后端尚未提供该能力')
  }

  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div
        className="modal desktop-release-modal"
        role="dialog"
        aria-modal="true"
        aria-label="新增发布"
      >
        <form onSubmit={submit} noValidate>
          <div className="modal-head">
            <div>
              <span className="modal-eyebrow">NEW RELEASE</span>
              <h2>新增发布</h2>
            </div>
            <button
              type="button"
              className="button subtle small"
              aria-label="关闭"
              onClick={onClose}
            >
              ×
            </button>
          </div>

          <div className="modal-body">
            <div className="desktop-release-fields">
              <label className="field">
                <span>版本号</span>
                <input
                  value={version}
                  placeholder="例如 0.2.0"
                  maxLength={32}
                  onChange={(event) => setVersion(event.target.value)}
                />
              </label>
              <label className="field">
                <span>平台</span>
                <select
                  value={platform}
                  onChange={(event) => setPlatform(event.target.value as Platform)}
                >
                  <option value="windows">Windows</option>
                  <option value="macos">macOS</option>
                </select>
              </label>
              <label className="field">
                <span>架构</span>
                <select value={arch} onChange={(event) => setArch(event.target.value as Arch)}>
                  <option value="x64">x64</option>
                  <option value="arm64">arm64</option>
                </select>
              </label>
              <label className="field wide">
                <span>更新说明（选填）</span>
                <textarea
                  rows={4}
                  maxLength={5000}
                  value={notes}
                  placeholder="本次版本的主要变更"
                  onChange={(event) => setNotes(event.target.value)}
                />
              </label>
            </div>

            <label
              className="desktop-dropzone"
              onDragOver={(event) => event.preventDefault()}
              onDrop={(event) => {
                event.preventDefault()
                addFiles(event.dataTransfer.files)
              }}
            >
              <strong>选择或拖拽文件到这里</strong>
              <span>支持 {ACCEPT_BY_PLATFORM[platform]}，可多选</span>
              <input
                type="file"
                multiple
                accept={ACCEPT_BY_PLATFORM[platform]}
                onChange={(event) => {
                  addFiles(event.target.files)
                  event.target.value = ''
                }}
              />
            </label>

            {files.length > 0 ? (
              <div className="desktop-file-section">
                <div className="desktop-file-section-heading">
                  <span>待上传文件</span>
                  <span className="chip">{files.length}</span>
                </div>
                <ul className="desktop-pending-files">
                  {files.map((file) => (
                    <li className="desktop-file-item" key={file.id}>
                      <span className="desktop-file-name">{file.name}</span>
                      <span className="desktop-file-meta">{formatBytes(file.size)}</span>
                      <button
                        type="button"
                        className="dependency-admin-action danger"
                        onClick={() => removeFile(file.id)}
                      >
                        移除
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}

            {notice ? (
              <p className="notice warn" role="status">
                {notice}
              </p>
            ) : null}
          </div>

          <div className="modal-foot">
            <button type="button" className="button subtle" onClick={onClose}>
              取消
            </button>
            <button
              type="submit"
              className="button primary"
              disabled={!version.trim() || files.length === 0}
            >
              上传文件
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default function DesktopReleases() {
  const [platform, setPlatform] = useState<'all' | Platform>('all')
  const [arch, setArch] = useState<'all' | Arch>('all')
  const [status, setStatus] = useState<'all' | ReleaseStatus>('all')
  const [searchInput, setSearchInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [creating, setCreating] = useState(false)

  const filtered = useMemo(() => {
    const needle = keyword.trim().toLowerCase()
    return RELEASES.filter((row) => {
      if (platform !== 'all' && row.platform !== platform) return false
      if (arch !== 'all' && row.arch !== arch) return false
      if (status !== 'all' && row.status !== status) return false
      if (!needle) return true
      return (
        row.version.toLowerCase().includes(needle) ||
        row.notes.toLowerCase().includes(needle) ||
        PLATFORM_LABELS[row.platform].toLowerCase().includes(needle)
      )
    })
  }, [platform, arch, status, keyword])

  const submitSearch = (event: FormEvent) => {
    event.preventDefault()
    setKeyword(searchInput)
  }

  return (
    <AppShell title="版本发布" breadcrumb="桌面版 / 版本发布">
      <div className="desktop-page-stack">
        <section className="desktop-page-intro">
          <span className="desktop-eyebrow">DESKTOP RELEASES</span>
          <h1>桌面应用发布</h1>
          <p>管理 Windows 和 macOS 的安装包版本，分平台上传并统一发布。</p>
        </section>

        <section className="panel">
          <div className="desktop-release-toolbar">
            <div className="desktop-release-filters">
              <label className="field">
                <span>平台</span>
                <select
                  value={platform}
                  onChange={(event) => setPlatform(event.target.value as 'all' | Platform)}
                >
                  <option value="all">全部平台</option>
                  <option value="windows">Windows</option>
                  <option value="macos">macOS</option>
                </select>
              </label>
              <label className="field">
                <span>架构</span>
                <select
                  value={arch}
                  onChange={(event) => setArch(event.target.value as 'all' | Arch)}
                >
                  <option value="all">全部架构</option>
                  <option value="x64">x64</option>
                  <option value="arm64">arm64</option>
                </select>
              </label>
              <label className="field">
                <span>状态</span>
                <select
                  value={status}
                  onChange={(event) =>
                    setStatus(event.target.value as 'all' | ReleaseStatus)
                  }
                >
                  <option value="all">全部状态</option>
                  <option value="draft">草稿</option>
                  <option value="partial">部分上传</option>
                  <option value="ready">待发布</option>
                  <option value="published">已发布</option>
                  <option value="withdrawn">已撤回</option>
                </select>
              </label>
            </div>

            <form className="dependency-admin-search desktop-release-search" onSubmit={submitSearch}>
              <IconSearch width={15} height={15} />
              <input
                value={searchInput}
                placeholder="搜索版本号"
                aria-label="搜索版本号"
                onChange={(event) => setSearchInput(event.target.value)}
              />
            </form>

            <button type="button" className="button primary" onClick={() => setCreating(true)}>
              <IconPlus width={15} height={15} /> 新增发布
            </button>
          </div>

          <div className="desktop-record-heading">
            <h2>发布记录</h2>
            <span>{filtered.length} 个版本</span>
          </div>

          {filtered.length === 0 ? (
            <div className="empty-inline dependency-admin-empty">
              <EmptyState
                title="暂无发布记录"
                description="本地后端尚未提供该能力。页面结构已按阿拉丁设计还原，接入接口后即可上传并发布安装包。"
                action={
                  <button
                    type="button"
                    className="button primary"
                    onClick={() => setCreating(true)}
                  >
                    新增发布
                  </button>
                }
              />
            </div>
          ) : (
            <div className="desktop-release-list">
              {filtered.map((row) => (
                <article className="desktop-release-card" key={row.id}>
                  <div className="desktop-release-card-head">
                    <h3>v{row.version}</h3>
                    <StateTag tone={STATUS_TONES[row.status]}>
                      {STATUS_LABELS[row.status]}
                    </StateTag>
                  </div>
                  {row.notes ? <p className="desktop-release-notes">{row.notes}</p> : null}
                  <div className="desktop-release-meta">
                    <span>
                      {PLATFORM_LABELS[row.platform]} · {row.arch}
                    </span>
                    <span>
                      {row.fileCount} 个文件 · {formatBytes(row.size)}
                    </span>
                    <span>{formatDate(row.createdAt)}</span>
                  </div>
                  <div className="desktop-release-actions">
                    {row.status !== 'published' && row.status !== 'withdrawn' ? (
                      <>
                        <button
                          type="button"
                          className="button ghost small"
                          onClick={() => toast('本地后端尚未提供该能力')}
                        >
                          继续上传
                        </button>
                        <button
                          type="button"
                          className="button primary small"
                          disabled={row.status !== 'ready'}
                          onClick={() => toast('本地后端尚未提供该能力')}
                        >
                          发布
                        </button>
                        <button
                          type="button"
                          className="button danger small"
                          onClick={() => toast('本地后端尚未提供该能力')}
                        >
                          删除
                        </button>
                      </>
                    ) : null}
                    {row.status === 'published' ? (
                      <button
                        type="button"
                        className="button ghost small"
                        onClick={() => toast('本地后端尚未提供该能力')}
                      >
                        撤回
                      </button>
                    ) : null}
                  </div>
                </article>
              ))}
            </div>
          )}
        </section>
      </div>

      {creating ? <ReleaseModal onClose={() => setCreating(false)} /> : null}
    </AppShell>
  )
}
