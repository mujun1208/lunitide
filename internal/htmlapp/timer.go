package htmlapp

import "strings"

const timerFallbackTitle = "计时器"

func renderTimer(title string) string {
	return strings.ReplaceAll(timerHTML, "{{TITLE}}", safeTitle(title, timerFallbackTitle))
}

const timerHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{TITLE}}</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
html,body{height:100%;background:#10141c;color:#eef3ff;font-family:"Segoe UI","Microsoft YaHei",sans-serif}
#wrap{min-height:100%;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:24px;padding:24px}
h1{font-size:22px;letter-spacing:1px}
#clock{font-size:72px;font-variant-numeric:tabular-nums;letter-spacing:4px}
.row{display:flex;gap:8px;flex-wrap:wrap;justify-content:center}
button,input{padding:8px 14px;border:0;border-radius:8px;font:inherit}
button{background:#3fd6ff;color:#071018;font-weight:700;cursor:pointer}
input{width:88px;background:#1b2230;color:#eef3ff}
</style>
</head>
<body>
<div id="wrap">
<h1>{{TITLE}}</h1>
<div id="clock">05:00</div>
<div class="row">
<input id="mins" type="number" min="0" max="180" value="5" aria-label="分钟">
<button id="start">开始</button>
<button id="pause">暂停</button>
<button id="reset">复位</button>
</div>
</div>
<script>
(function(){
var left=300,tick=null,clock=document.getElementById("clock");
function show(){var m=Math.floor(left/60),s=left%60;clock.textContent=(m<10?"0":"")+m+":"+(s<10?"0":"")+s}
function stop(){if(tick){clearInterval(tick);tick=null}}
document.getElementById("start").onclick=function(){
  if(tick)return;
  var n=parseInt(document.getElementById("mins").value,10);
  if(!tick && left===300 || clock.textContent==="00:00"){if(isFinite(n)&&n>=0)left=n*60}
  tick=setInterval(function(){if(left<=0){stop();clock.textContent="00:00";return}left--;show()},1000);
};
document.getElementById("pause").onclick=stop;
document.getElementById("reset").onclick=function(){
  stop();
  var n=parseInt(document.getElementById("mins").value,10);
  left=(isFinite(n)&&n>=0)?n*60:300;
  show();
};
show();
})();
</script>
</body>
</html>
`
