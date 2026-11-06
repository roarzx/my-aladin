import { useState } from 'react'

import AppShell from '../components/AppShell'
import { EmptyState, Panel, toast } from '../components/ui'
import { Link } from '../router'
import '../styles/pages-issues.css'

export default function CreateTask() {
  const [step, setStep] = useState<1 | 2>(1)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [nameError, setNameError] = useState('')

  const goNext = () => {
    if (!name.trim()) {
      setNameError('请输入任务名称')
      return
    }
    setNameError('')
    setStep(2)
  }

  return (
    <AppShell title="创建开发任务" breadcrumb="我的 Issue / 创建任务">
      <div className="create-steps">
        <div className={step === 1 ? 'create-step active' : 'create-step'}>
          <span className="step-index">1</span>
          <span>任务信息</span>
        </div>
        <div className={step === 2 ? 'create-step active' : 'create-step'}>
          <span className="step-index">2</span>
          <span>执行机器与 Agent</span>
        </div>
      </div>

      {step === 1 ? (
        <Panel
          title="任务信息"
          subtitle="填写任务名称与需求描述，创建后进入驾驶舱跟进执行。"
        >
          <div className="form-grid">
            <label className="field">
              <span>任务名称</span>
              <input
                value={name}
                onChange={(event) => {
                  setName(event.target.value)
                  if (nameError) setNameError('')
                }}
                placeholder="例如：修复登录页验证码无法刷新"
                aria-invalid={Boolean(nameError)}
              />
              {nameError ? (
                <small className="error">{nameError}</small>
              ) : (
                <small>用于在 Issue 列表与驾驶舱中识别任务。</small>
              )}
            </label>

            <label className="field">
              <span>模板</span>
              <select defaultValue="" disabled>
                <option value="">暂无可用模板</option>
              </select>
              <small>本地后端尚未提供模板列表接口。</small>
            </label>

            <label className="field">
              <span>应用</span>
              <select defaultValue="" disabled>
                <option value="">暂无可用应用</option>
              </select>
              <small>本地后端尚未提供应用列表接口。</small>
            </label>

            <label className="field wide">
              <span>需求描述</span>
              <textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                placeholder="补充背景、验收标准与相关链接。"
              />
            </label>
          </div>

          <div className="form-actions">
            <button
              type="button"
              className="button ghost"
              onClick={() => toast('本地后端尚未提供该能力')}
            >
              另存为项目
            </button>
            <button type="button" className="button primary" onClick={goNext}>
              下一步：选择执行机器
            </button>
          </div>
        </Panel>
      ) : (
        <Panel
          title="执行机器与 Agent"
          subtitle="选择执行机器与执行智能体后创建任务。"
        >
          <div className="alert-banner warn">
            <strong>⚠ 未选择执行机器</strong>
            <span>请选择一台机器来执行此任务。</span>
          </div>

          <div className="stack">
            <div className="create-section-title">
              选择执行机器
              <span className="create-note">（后台将自动启动云端环境）</span>
            </div>
            <EmptyState
              title="暂未接入机器列表"
              description="本地后端尚未提供机器列表接口，暂时无法选择执行机器。"
            />

            <div className="create-section-title">
              选择执行智能体 <span className="required">* 必选</span>
              <span className="create-note">（等待机器上线后自动开始）</span>
            </div>
            <EmptyState
              title="暂无可用智能体"
              description="本地后端尚未提供 Agent 配置接口，请先创建 Agent 配置后再选择。"
              action={
                <Link className="button ghost" to="/agents">
                  创建 Agent 配置
                </Link>
              }
            />
          </div>

          <div className="form-actions">
            <button type="button" className="button ghost" onClick={() => setStep(1)}>
              上一步
            </button>
            <button
              type="button"
              className="button primary"
              onClick={() => toast('本地后端尚未提供创建接口')}
            >
              创建任务并进入驾驶舱
            </button>
          </div>
        </Panel>
      )}
    </AppShell>
  )
}
