import { ReloadOutlined } from '@ant-design/icons'
import { ProCard } from '@ant-design/pro-components'
import { Alert, Button, Flex, Form, Input, Spin, Typography } from 'antd'
import AccessSettings from '../components/AccessSettings'
import { useSiteTitleForm, type SiteSettingsProps } from '../useSiteTitleForm'

export default function SiteSettings(props: SiteSettingsProps) {
  const { siteTitle, titleLoading, titleError, onLoadTitle, onExpired } = props
  const { draft, saving, saveError, saved, tooLong, canSave, changeTitle, save } = useSiteTitleForm(props)

  return (
    <Flex vertical gap="middle">
      <ProCard bordered title="网站标题">
        <Flex vertical gap="middle" align="start">
          <Typography.Text id="site-title-help" type="secondary">显示在登录页、后台和浏览器标签页，最多 80 个字符。</Typography.Text>
          {titleLoading ? <Spin size="small" aria-label="正在读取网站标题" /> : siteTitle ? (
            <Form layout="vertical" disabled={saving} onFinish={save} style={{ width: '100%' }}>
              <Flex vertical gap="middle" align="start">
                <Input
                  value={draft}
                  aria-label="网站标题"
                  aria-describedby={tooLong ? 'site-title-limit' : 'site-title-help'}
                  aria-invalid={tooLong || undefined}
                  status={tooLong ? 'error' : undefined}
                  onChange={(event) => changeTitle(event.target.value)}
                />
                {tooLong && <Typography.Text id="site-title-limit" type="danger">网站标题最多 80 个字符。</Typography.Text>}
                {saveError && <Alert type="error" showIcon message={saveError} />}
                {saved && <Alert type="success" showIcon role="status" message="网站标题已保存。" />}
                <Button htmlType="submit" type="primary" loading={saving} disabled={!canSave}>保存标题</Button>
              </Flex>
            </Form>
          ) : (
            <>
              {titleError && <Alert type="error" showIcon message={titleError} />}
              <Button icon={<ReloadOutlined />} onClick={() => void onLoadTitle()}>重试</Button>
            </>
          )}
        </Flex>
      </ProCard>
      <AccessSettings onExpired={onExpired} />
    </Flex>
  )
}
