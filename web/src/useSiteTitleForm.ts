import { useEffect, useRef, useState } from 'react'
import { api, APIError, errorMessage, type SiteSettings } from './api'

export interface SiteSettingsProps {
  siteTitle?: string
  titleLoading: boolean
  titleError: string
  onLoadTitle: () => Promise<void>
  onTitleSaved: (title: string) => void
  onExpired: () => void
}

export function useSiteTitleForm({ siteTitle, onTitleSaved, onExpired }: SiteSettingsProps) {
  const [draft, setDraft] = useState(siteTitle ?? '')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [saved, setSaved] = useState(false)
  const pendingSave = useRef(false)
  const title = draft.trim()
  const tooLong = Array.from(title).length > 80
  const canSave = Boolean(siteTitle && title && title !== siteTitle && !tooLong)

  useEffect(() => {
    setDraft(siteTitle ?? '')
  }, [siteTitle])

  function changeTitle(value: string) {
    setDraft(value)
    setSaveError('')
    setSaved(false)
  }

  async function save() {
    if (pendingSave.current || !canSave) return
    pendingSave.current = true
    setSaving(true)
    setSaveError('')
    setSaved(false)
    try {
      const current = await api<SiteSettings>('/site', 'PUT', { title })
      onTitleSaved(current.title)
      setSaved(true)
    } catch (failure) {
      if (failure instanceof APIError && failure.status === 401) onExpired()
      else setSaveError(errorMessage(failure))
    } finally {
      pendingSave.current = false
      setSaving(false)
    }
  }

  return { draft, saving, saveError, saved, tooLong, canSave, changeTitle, save }
}
