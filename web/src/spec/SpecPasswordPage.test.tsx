import { afterEach, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { SpecPasswordPage } from './SpecPasswordPage'

const changePassword = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))
vi.mock('../auth/AuthProvider', () => ({
  useAuth: () => ({ session: { username: 'admin' }, changePassword }),
}))

afterEach(() => changePassword.mockClear())

it('requires current password and matching new passwords before submitting', async () => {
  render(<SpecPasswordPage />)
  fireEvent.change(screen.getByLabelText('当前密码'), { target: { value: 'old-admin-password' } })
  fireEvent.change(screen.getByLabelText('新密码'), { target: { value: 'new-admin-password-2026' } })
  fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'different-password' } })
  fireEvent.click(screen.getByRole('button', { name: '修改密码' }))
  await waitFor(() => expect(screen.getByText('两次输入的新密码不一致')).toBeInTheDocument())
  expect(changePassword).not.toHaveBeenCalled()
  fireEvent.change(screen.getByLabelText('确认新密码'), { target: { value: 'new-admin-password-2026' } })
  fireEvent.click(screen.getByRole('button', { name: '修改密码' }))
  await waitFor(() => expect(changePassword).toHaveBeenCalledWith('old-admin-password', 'new-admin-password-2026', 'new-admin-password-2026'))
})
