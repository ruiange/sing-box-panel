<script setup lang="ts">
import { onBeforeUnmount, onMounted, shallowRef, watch } from 'vue'
import { init, use, type EChartsType } from 'echarts/core'
import { BarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { DailyUsage, HourlyUsage } from '../api'
import { formatBytes, formatTime, shortDay } from '../format'

use([BarChart, GridComponent, TooltipComponent, CanvasRenderer])

const props = defineProps<{ daily?: DailyUsage[]; hourly?: HourlyUsage[]; timeZone?: string }>()
const target = shallowRef<HTMLElement>()
let chart: EChartsType | undefined
let observer: ResizeObserver | undefined

function draw() {
  const hours = props.hourly
  const days = props.daily ?? []
  const values = hours ?? days
  chart?.setOption({
    animation: !window.matchMedia('(prefers-reduced-motion: reduce)').matches,
    grid: { left: 58, right: 12, top: 12, bottom: 30 },
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow', shadowStyle: { color: 'rgba(222, 238, 222, .045)' } },
      renderMode: 'richText',
      backgroundColor: '#252a24',
      borderColor: '#40483e',
      textStyle: { color: '#e5ece1', fontSize: 12 },
      valueFormatter: (value: unknown) => formatBytes(Number(value)),
    },
    xAxis: {
      type: 'category',
      data: hours ? hours.map((hour) => formatTime(hour.at, props.timeZone)) : days.map((day) => shortDay(day.date)),
      axisLine: { lineStyle: { color: '#353b34' } },
      axisTick: { show: false },
      axisLabel: { color: '#929b91', hideOverlap: true, margin: 12 },
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      splitNumber: 4,
      axisLabel: { color: '#929b91', formatter: (value: number) => formatBytes(value), hideOverlap: true },
      splitLine: { lineStyle: { color: '#30352e', type: 'dashed' } },
    },
    series: [
      {
        name: '下载',
        type: 'bar',
        barGap: '18%',
        barMaxWidth: 24,
        itemStyle: { color: '#b8f36e', borderRadius: [2, 2, 0, 0] },
        data: values.map((point) => Number(point.download)),
      },
      {
        name: '上传',
        type: 'bar',
        barMaxWidth: 24,
        itemStyle: { color: '#6c84b5', borderRadius: [3, 3, 0, 0] },
        data: values.map((point) => Number(point.upload)),
      },
    ],
  }, true)
}

onMounted(() => {
  if (!target.value) return
  chart = init(target.value, undefined, { renderer: 'canvas' })
  observer = new ResizeObserver(() => chart?.resize())
  observer.observe(target.value)
  draw()
})
watch(() => [props.daily, props.hourly, props.timeZone], draw)
onBeforeUnmount(() => {
  observer?.disconnect()
  chart?.dispose()
})
</script>

<template>
  <div ref="target" class="traffic-chart" role="img" :aria-label="hourly ? '每小时上传和下载流量柱状图' : '每日上传和下载流量柱状图'"></div>
</template>
