import { CloudServerOutlined, LockOutlined } from '@ant-design/icons'
import { LoginForm, ProFormText } from '@ant-design/pro-components'
import { Alert, Spin } from 'antd'

interface PanelLoginProps {
  siteTitle: string
  checking: boolean
  error: string
  onLogin: (password: string) => Promise<boolean>
}

export function PanelBrand() {
  return <span className="panel-brand" aria-hidden="true"><CloudServerOutlined /></span>
}

export default function PanelLogin({ siteTitle, checking, error, onLogin }: PanelLoginProps) {
  return (
    <main className="panel-login">
      <section className="panel-login-card" aria-label="管理员登录">
        {checking ? (
          <div className="panel-login-pending" role="status"><Spin />正在连接面板…</div>
        ) : (
          <LoginForm<{ password: string }>
            logo={<PanelBrand />}
            title={siteTitle}
            subTitle="管理员登录"
            contentStyle={{ width: '100%' }}
            onFinish={({ password }) => onLogin(password)}
            submitter={{ searchConfig: { submitText: '登录' }, submitButtonProps: { size: 'large' } }}
          >
            {error && <Alert className="panel-login-error" type="error" showIcon message={error} />}
            <ProFormText.Password
              name="password"
              label="管理员密码"
              fieldProps={{ autoComplete: 'current-password', autoFocus: true, size: 'large', prefix: <LockOutlined /> }}
              rules={[{ required: true, message: '请输入管理员密码' }]}
            />
          </LoginForm>
        )}
      </section>
    </main>
  )
}
