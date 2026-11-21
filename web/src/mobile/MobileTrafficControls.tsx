import { Button, NoticeBar, Popup, SearchBar, Skeleton } from 'antd-mobile'
import type { ButtonRef } from 'antd-mobile'
import { useId, useRef, useState } from 'react'
import type { Device, Traffic } from '../api'
import { formatTime } from '../format'
import { ranges, type HistoryRange } from '../trafficOptions'
import type { useLiveTraffic } from '../useLiveTraffic'

export interface MobileTrafficDataProps {
  devices: Device[]
  onTraffic: (id: string) => void
  traffic?: Traffic
  chartData?: Traffic
  range: HistoryRange
  onRangeChange: (range: HistoryRange) => void
  loading: boolean
  deviceError: string
  trafficError?: string
  live: ReturnType<typeof useLiveTraffic>
}

interface FilterProps<Value extends string> {
  label: string
  value: Value
  options: ReadonlyArray<{ value: Value; label: string }>
  onChange: (value: Value) => void
  searchPlaceholder?: string
}

function TrafficFilter<Value extends string>({
  label, value, options, onChange, searchPlaceholder,
}: FilterProps<Value>) {
  const id = useId()
  const trigger = useRef<ButtonRef>(null)
  const closeButton = useRef<ButtonRef>(null)
  const hasOpened = useRef(false)
  const [visible, setVisible] = useState(false)
  const [query, setQuery] = useState('')
  const selected = options.find((option) => option.value === value)
  const search = query.trim().toLocaleLowerCase()
  const filtered = search ? options.filter((option) => option.label.toLocaleLowerCase().includes(search)) : options

  function close() {
    setVisible(false)
  }

  return (
    <>
      <Button
        ref={trigger}
        className="mobile-traffic-filter"
        aria-label={`${label}，${selected?.label ?? '请选择'}`}
        aria-haspopup="dialog"
        aria-expanded={visible}
        onClick={() => {
          hasOpened.current = true
          setQuery('')
          setVisible(true)
        }}
      >
        <span className="mobile-traffic-filter-content">
          <span>{label}</span>
          <strong>{selected?.label ?? '请选择'}</strong>
        </span>
        <span className="mobile-traffic-filter-chevron" aria-hidden="true" />
      </Button>
      <Popup
        visible={visible}
        closeOnMaskClick
        onClose={close}
        afterShow={() => closeButton.current?.nativeElement?.focus()}
        afterClose={() => {
          if (!hasOpened.current) return
          hasOpened.current = false
          trigger.current?.nativeElement?.focus()
        }}
        bodyClassName="mobile-traffic-filter-body"
      >
        <section
          role="dialog"
          aria-modal="true"
          aria-labelledby={id}
          className="mobile-traffic-filter-sheet"
          onKeyDown={(event) => {
            if (event.key === 'Escape') {
              event.stopPropagation()
              close()
              return
            }
            if (event.key !== 'Tab') return
            const items = event.currentTarget.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled])')
            const first = items.item(0)
            const last = items.item(items.length - 1)
            const destination = event.shiftKey && document.activeElement === first ? last
              : !event.shiftKey && document.activeElement === last ? first : null
            if (destination) {
              event.preventDefault()
              destination.focus()
            }
          }}
        >
          <header className="mobile-traffic-filter-heading">
            <h2 id={id}>{label}</h2>
            <Button ref={closeButton} color="primary" fill="none" onClick={close}>关闭</Button>
          </header>
          {searchPlaceholder && (
            <SearchBar
              className="mobile-traffic-filter-search"
              placeholder={searchPlaceholder}
              value={query}
              onChange={setQuery}
            />
          )}
          <div className="mobile-traffic-filter-options">
            {filtered.map((option) => (
              <Button
                className="mobile-traffic-filter-option"
                color={option.value === value ? 'primary' : 'default'}
                fill="none"
                block
                key={option.value}
                aria-pressed={option.value === value}
                onClick={() => {
                  onChange(option.value)
                  close()
                }}
              >
                <span>{option.label}</span>
                {option.value === value && <small aria-hidden="true">已选</small>}
              </Button>
            ))}
            {!filtered.length && <p className="mobile-traffic-caption">没有匹配的设备</p>}
          </div>
        </section>
      </Popup>
    </>
  )
}

export function MobileTrafficRange({
  value, onChange,
}: { value: HistoryRange; onChange: (value: HistoryRange) => void }) {
  return <TrafficFilter label="统计范围" value={value} options={ranges} onChange={onChange} />
}

export function MobileTrafficScope({
  value, devices, onChange,
}: { value: string; devices: Device[]; onChange: (value: string) => void }) {
  const options = [
    { value: '', label: '全部设备' },
    ...devices.map((device) => ({ value: device.id, label: device.name + (device.enabled ? '' : '（已停用）') })),
  ]
  if (value && !options.some((option) => option.value === value)) {
    options.push({ value, label: '所选设备' })
  }
  return (
    <TrafficFilter
      label="统计设备"
      value={value}
      options={options}
      onChange={onChange}
      searchPlaceholder={devices.length > 6 ? '搜索设备名称' : undefined}
    />
  )
}

export function MobileTrafficNotices({
  deviceError, trafficError,
}: { deviceError?: string; trafficError?: string }) {
  return (
    <>
      {deviceError && (
        <div role="alert">
          <NoticeBar className="mobile-traffic-notice" color="error" wrap content={`设备列表读取失败。${deviceError}`} />
        </div>
      )}
      {trafficError && (
        <div role="alert">
          <NoticeBar className="mobile-traffic-notice" color="error" wrap content={`流量数据读取失败。${trafficError}`} />
        </div>
      )}
    </>
  )
}

export function MobileTrafficState({
  label, loading = false,
}: { label: string; loading?: boolean }) {
  return (
    <div className="mobile-traffic-state" role={loading ? 'status' : undefined}>
      {loading && <Skeleton.Paragraph animated lineCount={2} aria-hidden="true" />}
      <p>{label}</p>
    </div>
  )
}

export function MobileTrafficCoverage({ chartData }: { chartData?: Traffic }) {
  if (!chartData?.hourlySince) return null
  return (
    <p className="mobile-traffic-caption">
      小时统计自 <time dateTime={chartData.hourlySince}>{formatTime(chartData.hourlySince, chartData.timeZone)}</time> 起
    </p>
  )
}
