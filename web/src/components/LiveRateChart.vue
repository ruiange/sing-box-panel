<script setup lang="ts">
import { onBeforeUnmount, onMounted, shallowRef, watch } from 'vue'
import { init, use, type EChartsType } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { LiveRatePoint } from '../api'
import { formatRate } from '../format'

use([LineChart, GridComponent, TooltipComponent, CanvasRenderer])

const props = defineProps<{ history: LiveRatePoint[]; end: number }>()
const target = shallowRef<HTMLElement>()
let chart: EChartsType | undefined
let observer: ResizeObserver | undefined

function draw() {
  const start = props.end - 60000
  const points = props.history.map((sample) => ({ ...sample, time: Date.parse(sample.at) }))
    .filter((sample) => sample.time >= start && sample.time <= props.end)
  function values(direction: 'upload' | 'download') {
    const data: [number, number | null][] = []
    for (const sample of points) {
      const seconds = (sample.time - start) / 1000
      const previous = data.at(-1)
      if (previous && seconds - previous[0] > 4) data.push([previous[0] + 1, null])
      data.push([seconds, sample[direction]])
    }
    return data
  }

  chart?.setOption({
    animation: false,
    grid: { left: 76, right: 19, top: 12, bottom: 30 },
    tooltip: {
      trigger: 'axis',
      renderMode: 'richText',
      backgroundColor: '#252a24',
      borderColor: '#40483e',
      textStyle: { color: '#e5ece1', fontSize: 12 },
      axisPointer: { label: { formatter: ({ value }: { value: number }) => Math.round(60 - value) + ' 秒前' } },
      valueFormatter: (value: unknown) => value == null ? '—' : formatRate(Number(value)),
    },
    xAxis: {
      type: 'value',
      min: 0,
      max: 60,
      interval: 15,
      axisLine: { onZero: false, lineStyle: { color: '#353b34' } },
      axisTick: { show: false },
      splitLine: { show: false },
      axisLabel: {
        color: '#929b91',
        hideOverlap: true,
        margin: 12,
        formatter: (value: number) => value === 60 ? '现在' : (60 - value) + ' 秒前',
      },
    },
    yAxis: {
      type: 'value',
      min: 0,
      minInterval: 1,
      splitNumber: 4,
      axisLabel: { color: '#929b91', formatter: formatRate, hideOverlap: true },
      splitLine: { lineStyle: { color: '#30352e', type: 'dashed' } },
    },
    series: [
      {
        name: '下载',
        type: 'line',
        symbol: 'circle',
        symbolSize: 4,
        lineStyle: { color: '#b8f36e', width: 2 },
        itemStyle: { color: '#b8f36e' },
        encode: { x: 0, y: 1, tooltip: [1] },
        data: values('download'),
      },
      {
        name: '上传',
        type: 'line',
        symbol: 'circle',
        symbolSize: 3,
        lineStyle: { color: '#6c84b5', width: 2, type: 'dashed' },
        itemStyle: { color: '#6c84b5' },
        encode: { x: 0, y: 1, tooltip: [1] },
        data: values('upload'),
      },
    ],
  })
}

onMounted(() => {
  if (!target.value) return
  chart = init(target.value, undefined, { renderer: 'canvas' })
  observer = new ResizeObserver(() => chart?.resize())
  observer.observe(target.value)
  draw()
})
watch(() => [props.history, props.end], draw)
onBeforeUnmount(() => {
  observer?.disconnect()
  chart?.dispose()
})
</script>

<template>
  <div ref="target" class="traffic-chart live-chart" role="img" aria-label="最近 60 秒的上传和下载速度曲线"></div>
</template>
