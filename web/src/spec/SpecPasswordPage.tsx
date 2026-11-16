import { useState } from 'react'
import { Alert, Button, Form, Input } from 'antd'
import { LockOutlined } from '@ant-design/icons'
import axios from 'axios'
import { useAuth } from '../auth/AuthProvider'

type Fields = { currentPassword: string; newPassword: string; confirmPassword: string }

export function SpecPasswordPage() {
  const [form] = Form.useForm<Fields>()
  const { session, changePassword } = useAuth()
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  async function submit(values: Fields) {
    setSubmitting(true)
    setError('')
    try {
      await changePassword(values.currentPassword, values.newPassword, values.confirmPassword)
    } catch (cause) {
      const code = axios.isAxiosError(cause) ? cause.response?.data?.code : undefined
      setError(code === 'current_password_incorrect' ? '当前密码错误，请重新输入。'
        : code === 'rate_limited' ? '尝试次数过多，请稍后再试。'
          : code === 'password_unchanged' ? '新密码不能与当前密码相同。'
            : code === 'password_mismatch' ? '两次输入的新密码不一致。'
              : '密码修改失败，请稍后重试。')
    } finally {
      form.resetFields()
      setSubmitting(false)
    }
  }

  return <div className="spec-password-page">
    <div className="spec-page-heading"><div><h1>修改密码</h1><p>为当前登录账号设置新密码</p></div></div>
    <section className="spec-panel spec-password-panel" aria-labelledby="spec-password-heading">
      <div className="spec-panel-head"><div><h2 id="spec-password-heading">账号安全</h2><p>当前账号：{session?.username}。修改成功后，所有已登录会话将退出。</p></div></div>
      <Form<Fields> form={form} layout="vertical" requiredMark={false} onFinish={submit} className="spec-password-form">
        <Form.Item name="currentPassword" label="当前密码" rules={[{ required: true, message: '请输入当前密码' }]}><Input.Password prefix={<LockOutlined />} autoComplete="current-password" maxLength={4096} /></Form.Item>
        <Form.Item name="newPassword" label="新密码" extra="至少 12 个字符，不能与当前密码相同。" rules={[{ required: true, message: '请输入新密码' }, { validator: async (_, value: string) => { if (value && [...value].length < 12) throw new Error('新密码至少需要 12 个字符') } }]}><Input.Password prefix={<LockOutlined />} autoComplete="new-password" maxLength={4096} /></Form.Item>
        <Form.Item name="confirmPassword" label="确认新密码" dependencies={['newPassword']} rules={[{ required: true, message: '请再次输入新密码' }, ({ getFieldValue }) => ({ validator: async (_, value: string) => { if (value && value !== getFieldValue('newPassword')) throw new Error('两次输入的新密码不一致') } })]}><Input.Password prefix={<LockOutlined />} autoComplete="new-password" maxLength={4096} /></Form.Item>
        {error && <Alert className="spec-password-error" type="error" showIcon title={error} role="alert" />}
        <div className="spec-password-actions"><Button type="primary" htmlType="submit" loading={submitting}>修改密码</Button></div>
      </Form>
    </section>
  </div>
}
