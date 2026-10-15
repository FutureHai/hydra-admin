import { useState } from 'react'
import { Alert, Button, Form, Input } from 'antd'
import { LockOutlined, SafetyCertificateOutlined, UserOutlined } from '@ant-design/icons'
import axios from 'axios'
import { Navigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthProvider'

type Fields = { username: string; password: string }

export function SpecLogin() {
  const [form] = Form.useForm<Fields>()
  const { loading, session, login } = useAuth()
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  async function submit(values: Fields) {
    setSubmitting(true)
    setError('')
    try {
      await login(values.username, values.password)
    } catch (cause) {
      setError(axios.isAxiosError(cause) && cause.response?.status === 429 ? '请求过于频繁，请稍后重试。' : '用户名或密码错误。')
    } finally {
      form.resetFields()
      setSubmitting(false)
    }
  }

  if (!loading && session) return <Navigate to="/" replace />

  return <main className="spec-login">
    <div className="spec-login-wrap">
      <div className="spec-login-identity"><span className="spec-login-mark"><SafetyCertificateOutlined /></span><div><strong>Hydra Admin Console</strong><span>OAuth 2.0 管理</span></div></div>
      <section className="spec-login-card" aria-labelledby="spec-login-title">
        <div className="spec-login-card-head"><h1 id="spec-login-title">管理员登录</h1><p>输入管理员凭据以访问控制台</p></div>
        <Form<Fields> form={form} layout="vertical" requiredMark={false} onFinish={submit} className="spec-login-form">
          <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]}><Input prefix={<UserOutlined />} autoComplete="username" placeholder="管理员用户名" /></Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}><Input.Password prefix={<LockOutlined />} autoComplete="current-password" placeholder="输入密码" /></Form.Item>
          <Button type="primary" htmlType="submit" block loading={submitting} disabled={loading}>登录</Button>
        </Form>
        {error && <Alert className="spec-login-feedback" type="error" showIcon title={error} role="alert" />}
      </section>
    </div>
  </main>
}
