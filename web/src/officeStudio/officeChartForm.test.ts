import { expect, it } from 'vitest';
import { chartFormError, type OfficeChartForm } from './officeChartForm';
const fixture = (): OfficeChartForm => ({ type: 'column', title: '销量', categories: ['一月'], series: [{ name: '本年', values: ['12345.1250000001'] }], x: 11, y: 22, width: 33, height: 44, legend: true });
it('accepts exact decimal strings without converting or rounding them', () => { const chart = fixture(); expect(chartFormError(chart)).toBe(''); expect(chart.series[0].values[0]).toBe('12345.1250000001'); });
it.each(['', 'NaN', '1e3', '3%', '01.5', ' 1.5'])('rejects noncanonical or missing numeric input: %s', value => { const chart = fixture(); chart.series[0].values = [value]; expect(chartFormError(chart)).toContain('十进制数值'); });
it('keeps all series intact when pie validation fails', () => { const chart = fixture(); chart.type = 'pie'; chart.series.push({ name: '去年', values: ['20'] }); expect(chartFormError(chart)).toContain('饼图只支持一个系列'); expect(chart.series).toHaveLength(2); });
it('checks actual UTF-8 label limits and negative pie values', () => { const chart = fixture(); chart.title = '字'.repeat(342); expect(chartFormError(chart)).toContain('1024 字节'); chart.title = ''; chart.type = 'pie'; chart.series[0].values = ['-2']; expect(chartFormError(chart)).toContain('负数'); chart.series[0].values = ['-0.000']; expect(chartFormError(chart)).toContain('大于零'); });
