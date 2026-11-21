import { useCallback, useEffect, useRef, useState } from 'react'
import { api, APIError, errorMessage, type SiteSettings } from './api'

const defaultSiteTitle = document.title

export function usePanelSession() {
  const [stage, setStage] = useState<'checking' | 'login' | 'ready'>('checking')
  const [loginError, setLoginError] = useState('')
  const [loggingOut, setLoggingOut] = useState(false)
  const [siteTitle, setSiteTitle] = useState<string>()
  const [titleLoading, setTitleLoading] = useState(true)
  const [titleError, setTitleError] = useState('')
  const titleRequest = useRef<AbortController>()

  const showLogin = useCallback((reason = '') => {
    setLoginError(reason)
    setStage('login')
  }, [])
  const onExpired = useCallback(() => showLogin('登录已过期，请重新登录。'), [showLogin])

  const loadSiteTitle = useCallback(async () => {
    titleRequest.current?.abort()
    const request = new AbortController()
    titleRequest.current = request
    setTitleLoading(true)
    setTitleError('')
    try {
      const current = await api<SiteSettings>('/site', 'GET', undefined, request.signal)
      if (!request.signal.aborted) setSiteTitle(current.title)
    } catch (error) {
      if (!request.signal.aborted) setTitleError(errorMessage(error))
    } finally {
      if (!request.signal.aborted) setTitleLoading(false)
    }
  }, [])

  const applySiteTitle = useCallback((title: string) => {
    titleRequest.current?.abort()
    setSiteTitle(title)
    setTitleError('')
    setTitleLoading(false)
  }, [])

  useEffect(() => {
    const request = new AbortController()
    void loadSiteTitle()
    void api<{ authenticated: boolean }>('/auth/session', 'GET', undefined, request.signal).then(
      (session) => {
        if (request.signal.aborted) return
        if (session?.authenticated) setStage('ready')
        else showLogin()
      },
      (error) => {
        if (!request.signal.aborted) showLogin(errorMessage(error))
      },
    )
    return () => {
      request.abort()
      titleRequest.current?.abort()
    }
  }, [loadSiteTitle, showLogin])

  const displayTitle = siteTitle ?? defaultSiteTitle
  useEffect(() => { document.title = displayTitle }, [displayTitle])

  async function login(password: string) {
    setLoginError('')
    try {
      const result = await api<{ authenticated: boolean }>('/auth/login', 'POST', { password })
      if (!result.authenticated) throw new Error('密码验证失败。')
      setStage('ready')
      return true
    } catch (error) {
      setLoginError(errorMessage(error))
      return false
    }
  }

  async function logout() {
    if (loggingOut) return
    setLoggingOut(true)
    try {
      await api('/auth/logout', 'POST')
      showLogin()
    } catch (error) {
      if (error instanceof APIError && error.status === 401) showLogin()
      else throw error
    } finally {
      setLoggingOut(false)
    }
  }

  return {
    stage, login, loginError, logout, loggingOut, onExpired,
    displayTitle, siteTitle, titleLoading, titleError, loadSiteTitle, applySiteTitle,
  }
}
