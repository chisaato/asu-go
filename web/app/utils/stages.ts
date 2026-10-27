export interface StageInfo {
  name: string
  label: string
  percent: number
  description?: string
}

export const STAGE_CONFIG: Record<string, { label: string, percent: number }> = {
  init: { label: '初始化', percent: 10 },
  container_setup: { label: '准备容器环境', percent: 25 },
  validate_revision: { label: '校验版本信息', percent: 40 },
  validate_manifest: { label: '校验依赖清单', percent: 55 },
  manifest: { label: '校验依赖清单', percent: 55 },
  building_image: { label: '编译构建固件', percent: 80 },
  build: { label: '编译构建固件', percent: 80 },
  collect: { label: '提取收集产物', percent: 92 },
  done: { label: '构建完成', percent: 100 }
}

export function stageInfo(stage: string | null | undefined): StageInfo {
  if (!stage) {
    return { name: '', label: '等待开始', percent: 0 }
  }
  const config = STAGE_CONFIG[stage]
  if (config) {
    return { name: stage, label: config.label, percent: config.percent }
  }
  return { name: stage, label: stage, percent: 50 }
}
