import { CloudServerOutlined, LockOutlined, UserOutlined } from '@ant-design/icons'
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
    <main className="panel-login" aria-label="管理员登录">
      <LoginForm<{ username: string; password: string }>
        name="admin-login"
        method="post"
        logo={<PanelBrand />}
        title={siteTitle}
        subTitle="管理员登录"
        contentStyle={{ width: 'min(100%, 328px)', minWidth: 0 }}
        message={checking ? (
          <div className="panel-login-pending" role="status"><Spin size="small" />正在连接面板…</div>
        ) : error ? (
          <Alert className="panel-login-error" type="error" showIcon message={error} />
        ) : undefined}
        initialValues={{ username: 'root' }}
        onFinish={({ password }) => onLogin(password)}
        submitter={checking ? false : { searchConfig: { submitText: '登录' } }}
      >
        {!checking && (
          <>
            <ProFormText
              name="username"
              label="管理员账号"
              fieldProps={{
                name: 'username', autoComplete: 'username', autoCapitalize: 'none', spellCheck: false,
                size: 'large', prefix: <UserOutlined aria-hidden="true" />,
              }}
              rules={[
                { required: true, message: '请输入管理员账号' },
                { pattern: /^root$/, message: '管理员账号为 root' },
              ]}
            />
            <ProFormText.Password
              name="password"
              label="管理员密码"
              fieldProps={{
                name: 'password', autoComplete: 'current-password',
                size: 'large', prefix: <LockOutlined aria-hidden="true" />,
              }}
              rules={[{ required: true, message: '请输入管理员密码' }]}
            />
          </>
        )}
      </LoginForm>
    </main>
  )
}
