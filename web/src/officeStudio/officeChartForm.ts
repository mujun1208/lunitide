export interface OfficeChartForm {
  type: 'column' | 'bar' | 'line' | 'pie'; title: string; categories: string[];
  series: Array<{ name: string; values: string[] }>; x: number; y: number; width: number; height: number; legend: boolean;
}
const decimal = /^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$/;
export function chartDecimalValid(value: string): boolean {
  return value.length <= 64 && decimal.test(value) && value.replace(/^-/, '').replace('.', '').replace(/^0+/, '').length <= 15;
}
const label = (text: string) => new TextEncoder().encode(text).length <= 1024 && !/[\u0000-\u0008\u000b\u000c\u000e-\u001f]/.test(text);
export function chartFormError(chart: OfficeChartForm): string {
  if (!label(chart.title)) return '标题不能超过 1024 字节或包含控制字符。';
  if (!chart.categories.length || chart.categories.length > 100) return '图表需为 1–100 个类别。';
  if (!chart.series.length || chart.series.length > 6) return '图表需为 1–6 个系列。';
  if (chart.categories.some(value => !label(value))) return '类别名称不能超过 1024 字节或包含控制字符。';
  for (const [index, series] of chart.series.entries()) {
    if (!series.name.trim() || !label(series.name)) return `请填写系列 ${index + 1} 的名称（最多 1024 字节）。`;
    if (series.values.length !== chart.categories.length) return `系列 ${index + 1} 的数值数量与类别不一致。`;
    for (const [row, value] of series.values.entries()) {
      if (value.length > 64 || !decimal.test(value)) return `系列 ${index + 1}、第 ${row + 1} 类需要十进制数值，例如 12.5；不能留空或带百分号。`;
      if (!chartDecimalValid(value)) return `系列 ${index + 1}、第 ${row + 1} 类超过 Excel 的 15 位数值精度；请将长编号用作类别文本。不会自动舍入。`;
    }
  }
  if (chart.type === 'pie') {
    if (chart.series.length !== 1) return '饼图只支持一个系列，请先保留需要展示的系列。';
    const values = chart.series[0].values;
    if (values.some(value => value.startsWith('-') && /[1-9]/.test(value))) return '饼图不能包含负数。';
    if (!values.some(value => /[1-9]/.test(value))) return '饼图至少需要一个大于零的数值。';
  }
  return '';
}
