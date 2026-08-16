(function () {
  var style = getComputedStyle(document.documentElement);
  var accent = style.getPropertyValue('--accent').trim();
  var accent2 = style.getPropertyValue('--accent2').trim();
  var ink = style.getPropertyValue('--ink').trim();
  var muted = style.getPropertyValue('--muted').trim();
  var rule = style.getPropertyValue('--rule').trim();
  var bg2 = style.getPropertyValue('--bg2').trim();
  var warn = style.getPropertyValue('--warn').trim();

  // ---------- Chart 1: radar ----------
  var radarEl = document.getElementById('chart-radar');
  if (radarEl) {
    var radar = echarts.init(radarEl, null, { renderer: 'svg' });
    radar.setOption({
      animation: false,
      color: [accent, warn, accent2, '#8a6d1f'],
      tooltip: { appendToBody: true },
      legend: { bottom: 0, textStyle: { color: muted, fontSize: 12 } },
      radar: {
        indicator: [
          { name: '工具调用面', max: 5 },
          { name: 'MCP 生态', max: 5 },
          { name: '多 Agent 编排', max: 5 },
          { name: '定时自动化', max: 5 },
          { name: '办公产出', max: 5 },
          { name: '渠道与远程', max: 5 },
          { name: '安全治理', max: 5 },
          { name: '记忆与上下文', max: 5 }
        ],
        radius: '62%',
        center: ['50%', '48%'],
        axisName: { color: ink, fontSize: 12 },
        splitLine: { lineStyle: { color: rule } },
        splitArea: { areaStyle: { color: [bg2, '#f3f6fb'] } },
        axisLine: { lineStyle: { color: rule } }
      },
      series: [{
        type: 'radar',
        symbolSize: 5,
        data: [
          { value: [1.5, 1.5, 1.5, 0.5, 0.5, 0.5, 4.5, 3.5], name: 'Lunitide v0.3.3', lineStyle: { width: 3 }, areaStyle: { opacity: 0.18 } },
          { value: [5, 4.5, 5, 4, 2, 3, 4.5, 4], name: 'Codex', lineStyle: { width: 1.5 }, areaStyle: { opacity: 0.05 } },
          { value: [4.5, 4, 4, 4, 1, 5, 3.5, 3.5], name: 'OpenClaw', lineStyle: { width: 1.5 }, areaStyle: { opacity: 0.05 } },
          { value: [4, 4.5, 4, 4.5, 4, 4, 3.5, 3.5], name: 'Trae Work', lineStyle: { width: 1.5 }, areaStyle: { opacity: 0.05 } }
        ]
      }]
    });
    window.addEventListener('resize', function () { radar.resize(); });
  }

  // ---------- Chart 2: gap bar ----------
  var gapEl = document.getElementById('chart-gap');
  if (gapEl) {
    var dims = ['办公文档产出', '渠道与远程控制', '定时任务 / 自动化', '命令执行', '跨端部署', '工具调用面', '多 Agent 编排', '插件 / 技能市场', 'Agent 任务循环', 'MCP 生态接入', '交付验收闭环', '浏览器自动化', '文件沙箱与授权', '模型接入', '记忆系统', '安全治理'];
    var gaps = [4.5, 4.5, 4.0, 4.0, 4.0, 3.5, 3.5, 3.5, 3.0, 3.0, 3.0, 2.5, 2.0, 1.5, 1.0, 0.0];
    var gap = echarts.init(gapEl, null, { renderer: 'svg' });
    gap.setOption({
      animation: false,
      tooltip: {
        appendToBody: true,
        formatter: function (p) { return p.name + '<br/>差距指数：' + p.value.toFixed(1); }
      },
      grid: { left: 150, right: 60, top: 10, bottom: 30 },
      xAxis: {
        type: 'value', min: 0, max: 5,
        axisLabel: { color: muted },
        splitLine: { lineStyle: { color: rule } }
      },
      yAxis: {
        type: 'category', inverse: true, data: dims,
        axisLabel: { color: ink, fontSize: 12 },
        axisLine: { lineStyle: { color: rule } },
        axisTick: { show: false }
      },
      series: [{
        type: 'bar',
        data: gaps,
        barWidth: 16,
        label: {
          show: true, position: 'right', color: muted, fontFamily: 'JetBrains Mono, monospace',
          formatter: function (p) { return p.value.toFixed(1); }
        },
        itemStyle: {
          borderRadius: [0, 8, 8, 0],
          color: function (p) {
            if (p.value >= 3.5) { return warn; }
            if (p.value >= 2) { return '#c99216'; }
            if (p.value > 0) { return accent2; }
            return '#9db8a6';
          }
        }
      }]
    });
    window.addEventListener('resize', function () { gap.resize(); });
  }
})();
