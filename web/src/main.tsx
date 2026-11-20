import { App as AntApp, ConfigProvider, type ThemeConfig } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import 'antd/dist/reset.css'
import './style.css'

const theme: ThemeConfig = {
  token: {
    colorPrimary: '#145abb',
    colorText: '#172740',
    colorTextSecondary: '#52647e',
    colorBgLayout: '#f2f5fa',
    borderRadius: 8,
    fontFamily: '"PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif',
  },
  components: {
    Card: { headerHeight: 54 },
    Table: { headerBg: '#f5f8fc', headerColor: '#425671', rowHoverBg: '#f3f8ff' },
  },
}

createRoot(document.getElementById('app')!).render(
  <StrictMode>
    <ConfigProvider locale={zhCN} theme={theme}>
      <AntApp>
        <App />
      </AntApp>
    </ConfigProvider>
  </StrictMode>,
)
