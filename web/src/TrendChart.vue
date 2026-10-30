<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { init, use, type EChartsType } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { Daily } from './api'
use([LineChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer])
const props = defineProps<{ daily: Daily[] }>()
const host = ref<HTMLDivElement>()
let chart: EChartsType | undefined
let observer: ResizeObserver | undefined
function render() {
  chart?.setOption({
    color: ['#7664df', '#43ac9c', '#eeae5c'],
    tooltip: { trigger: 'axis', confine: true },
    legend: { top: 0, right: 0, icon: 'circle', itemWidth: 8, textStyle: { color: '#7b8192' } },
    grid: { top: 48, left: 48, right: 18, bottom: 30 },
    xAxis: { type: 'category', boundaryGap: false, data: props.daily.map(d => d.date), axisLine: { lineStyle: { color: '#e9eaf1' } }, axisTick: { show: false }, axisLabel: { color: '#9298aa', formatter: (s: string) => s.slice(5) } },
    yAxis: { type: 'value', minInterval: 1, axisLabel: { color: '#9298aa' }, splitLine: { lineStyle: { color: '#f0f1f6', type: 'dashed' } } },
    series: [
      { name: '日活跃访客', key: 'active' }, { name: '会话', key: 'sessions' }, { name: '页面浏览', key: 'page_views' },
    ].map((s, index) => ({ name: s.name, type: 'line', smooth: true, showSymbol: props.daily.length === 1, symbolSize: 7, lineStyle: { width: 2.5 }, areaStyle: index === 0 ? { opacity: 0.07 } : undefined, data: props.daily.map(d => d[s.key as keyof Daily]) })),
  }, true)
}
onMounted(() => { chart = init(host.value!); observer = new ResizeObserver(() => chart?.resize()); observer.observe(host.value!); render() })
watch(() => props.daily, render, { deep: true })
onBeforeUnmount(() => { observer?.disconnect(); chart?.dispose() })
</script>
<template><div ref="host" class="chart" role="img" aria-label="每日活跃访客、会话和页面浏览量趋势图"></div></template>
