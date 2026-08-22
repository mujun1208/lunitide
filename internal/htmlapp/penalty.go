package htmlapp

import "strings"

const penaltyFallbackTitle = "世界杯点球大战"

func renderPenalty(title string) string {
	return strings.ReplaceAll(penaltyHTML, "{{TITLE}}", safeTitle(title, penaltyFallbackTitle))
}

// Compact FIFA-style penalty shootout. Double-click the .html to play.
const penaltyHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{TITLE}}</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
html,body{height:100%;background:#071422;color:#f4f1e8;font-family:"Segoe UI","Microsoft YaHei",sans-serif;overflow:hidden}
#wrap{display:flex;flex-direction:column;height:100%}
header{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:10px 16px;background:linear-gradient(90deg,#0b1f3a,#123a2a);border-bottom:2px solid #c9a227}
h1{font-size:18px;letter-spacing:1px}
.hud{display:flex;gap:16px;font-size:14px}
.hud b{color:#f2d06b}
canvas{display:block;margin:auto;background:#0a5c32;max-width:100%;max-height:calc(100% - 88px)}
footer{padding:8px 16px;font-size:13px;color:#cde6d4;text-align:center}
button{margin-left:8px;padding:4px 10px;border:0;border-radius:6px;background:#c9a227;color:#1a1204;font-weight:700;cursor:pointer}
</style>
</head>
<body>
<div id="wrap">
<header>
<h1>{{TITLE}}</h1>
<div class="hud"><span>比分 <b id="score">0 - 0</b></span><span>轮次 <b id="round">1 / 5</b></span><span id="status">点击球门射门</span></div>
</header>
<canvas id="c" width="960" height="540"></canvas>
<footer>世界杯主题 · 点球大战 · 五轮定胜负，平局进入金球。点击球门方向射门，空格也可。<button id="reset" type="button">重赛</button></footer>
</div>
<script>
(function(){
var cv=document.getElementById("c"),ctx=cv.getContext("2d");
var W=cv.width,H=cv.height;
var scoreP=0,scoreK=0,round=1,sudden=false,shotsP=0,shotsK=0;
var phase="ready";
var ball={x:W/2,y:H-70,tx:W/2,ty:120,t:0};
var keeper={x:W/2,tx:W/2,dive:0};
var msg="点击球门射门";
var lastGoal=null;
function goalRect(){return {x:W/2-160,y:70,w:320,h:110};}
function resetBall(){ball.x=W/2;ball.y=H-70;ball.t=0;keeper.x=W/2;keeper.dive=0;phase="ready";}
function setHud(){
  document.getElementById("score").textContent=scoreP+" - "+scoreK;
  document.getElementById("round").textContent=sudden?("金球 "+round): (round+" / 5");
  document.getElementById("status").textContent=msg;
}
function canStillWin(me,them,left){return me+left>them;}
function remainingFor(whoShots){return sudden?1:Math.max(0,5-whoShots);}
function maybeEnd(){
  if(sudden){
    if(shotsP===shotsK && scoreP!==scoreK){phase="over";msg=(scoreP>scoreK?"你赢了！冠军奖杯属于你。":"门将扑出了冠军。再来一局？");setHud();return true;}
    return false;
  }
  var pLeft=remainingFor(shotsP),kLeft=remainingFor(shotsK);
  if(!canStillWin(scoreP,scoreK,pLeft) || !canStillWin(scoreK,scoreP,kLeft) || (shotsP>=5 && shotsK>=5)){
    if(scoreP===scoreK){sudden=true;round=1;shotsP=0;shotsK=0;msg="平局！进入金球决胜";phase="ready";resetBall();setHud();return false;}
    phase="over";msg=scoreP>scoreK?"你赢了！世界杯点球大战冠军。":"惜败。点重赛再踢一轮。";setHud();return true;
  }
  return false;
}
function shoot(mx,my){
  if(phase!=="ready")return;
  var g=goalRect();
  var tx=Math.max(g.x+12,Math.min(g.x+g.w-12,mx));
  var ty=Math.max(g.y+16,Math.min(g.y+g.h-10,my));
  ball.tx=tx;ball.ty=ty;ball.t=0;
  var bias=(Math.random()-0.5)*90;
  keeper.tx=Math.max(g.x+30,Math.min(g.x+g.w-30,tx+bias));
  keeper.dive=keeper.tx>keeper.x?1:-1;
  phase="flight";
  msg="看球！";
  setHud();
}
function finishShot(){
  var g=goalRect();
  var inGoal=ball.tx>g.x+8 && ball.tx<g.x+g.w-8 && ball.ty>g.y+8 && ball.ty<g.y+g.h-4;
  var saved=Math.abs(ball.tx-keeper.x)<38 && Math.abs(ball.ty-(g.y+g.h-18))<42;
  shotsP+=1;
  if(inGoal && !saved){scoreP+=1;lastGoal="goal";msg="进了！！";}
  else if(saved){lastGoal="save";msg="门将扑出！";}
  else {lastGoal="miss";msg="打偏了。";}
  phase="pause";
  setHud();
  setTimeout(function(){
    if(maybeEnd())return;
    round = sudden ? (shotsK>=shotsP?shotsP:round) : Math.min(5, Math.floor(shotsP)+ (shotsP===shotsK?1:0));
    if(!sudden) round=Math.min(5, Math.max(shotsP,shotsK)+(shotsP===shotsK?1:0));
    if(sudden) round=Math.max(shotsP,shotsK);
    cpuKick();
  },700);
}
function cpuKick(){
  if(phase==="over")return;
  phase="cpu";
  msg="对方主罚…";
  setHud();
  var g=goalRect();
  var tx=g.x+20+Math.random()*(g.w-40);
  var ty=g.y+20+Math.random()*(g.h-30);
  ball.x=W/2;ball.y=H-70;ball.tx=tx;ball.ty=ty;ball.t=0;
  keeper.x=W/2;
  keeper.tx=g.x+30+Math.random()*(g.w-60);
  var onTarget=Math.random()>0.22;
  if(!onTarget){ball.tx=Math.random()>0.5?g.x-20:g.x+g.w+20;}
  var diveWell=Math.random()>0.45;
  if(diveWell) keeper.tx=tx;
  keeper.dive=keeper.tx>keeper.x?1:-1;
  var start=performance.now();
  function stepCpu(now){
    var t=Math.min(1,(now-start)/620);
    ball.t=t;
    ball.x=W/2+(ball.tx-W/2)*t;
    ball.y=(H-70)+(ball.ty-(H-70))*t - Math.sin(t*Math.PI)*40;
    keeper.x+=(keeper.tx-keeper.x)*0.18;
    draw();
    if(t<1){requestAnimationFrame(stepCpu);return;}
    var inGoal=ball.tx>g.x+8 && ball.tx<g.x+g.w-8 && ball.ty>g.y+8 && ball.ty<g.y+g.h-4;
    var saved=Math.abs(ball.tx-keeper.x)<38;
    shotsK+=1;
    if(inGoal && !saved){scoreK+=1;msg="对方破门。";}
    else {msg=saved?"你扑住了！":"对方打偏。";}
    setHud();
    setTimeout(function(){
      if(maybeEnd())return;
      if(!sudden) round=Math.min(5, Math.max(shotsP,shotsK)+1);
      else round=Math.max(shotsP,shotsK)+1;
      resetBall();
      msg="轮到你了，点击球门射门";
      setHud();
      draw();
    },750);
  }
  requestAnimationFrame(stepCpu);
}
function tick(){
  if(phase==="flight"){
    ball.t=Math.min(1,ball.t+0.045);
    ball.x=W/2+(ball.tx-W/2)*ball.t;
    ball.y=(H-70)+(ball.ty-(H-70))*ball.t - Math.sin(ball.t*Math.PI)*48;
    keeper.x+=(keeper.tx-keeper.x)*0.16;
    if(ball.t>=1) finishShot();
  } else if(phase==="ready"){
    keeper.x=W/2+Math.sin(performance.now()/420)*42;
  }
  draw();
  requestAnimationFrame(tick);
}
function drawPitch(){
  var g=ctx.createLinearGradient(0,0,0,H);
  g.addColorStop(0,"#0b3d22");g.addColorStop(1,"#0e7a3b");
  ctx.fillStyle=g;ctx.fillRect(0,0,W,H);
  ctx.fillStyle="rgba(255,255,255,0.06)";
  for(var i=0;i<12;i++) ctx.fillRect(0,i*45,W,22);
  ctx.strokeStyle="rgba(255,255,255,0.55)";ctx.lineWidth=2;
  ctx.strokeRect(W/2-210,H-160,420,150);
  ctx.beginPath();ctx.arc(W/2,H-70,54,Math.PI,0);ctx.stroke();
  ctx.fillStyle="#6ec1ff";ctx.fillRect(0,0,W,64);
  ctx.fillStyle="#0a2744";ctx.fillRect(0,0,W,36);
}
function drawGoal(){
  var g=goalRect();
  ctx.fillStyle="#dfe7ee";
  ctx.fillRect(g.x-10,g.y,10,g.h+18);
  ctx.fillRect(g.x+g.w,g.y,10,g.h+18);
  ctx.fillRect(g.x-10,g.y-8,g.w+20,10);
  ctx.strokeStyle="rgba(255,255,255,0.35)";ctx.lineWidth=1;
  for(var x=g.x;x<g.x+g.w;x+=18){ctx.beginPath();ctx.moveTo(x,g.y);ctx.lineTo(x,g.y+g.h);ctx.stroke();}
  for(var y=g.y;y<g.y+g.h;y+=16){ctx.beginPath();ctx.moveTo(g.x,y);ctx.lineTo(g.x+g.w,y);ctx.stroke();}
  ctx.fillStyle="#c9a227";ctx.font="bold 16px sans-serif";ctx.textAlign="center";
  ctx.fillText("FIFA WORLD CUP",W/2,28);
}
function drawKeeper(){
  var g=goalRect();
  var x=keeper.x,y=g.y+g.h-8;
  ctx.save();
  ctx.translate(x,y);
  ctx.rotate(keeper.dive*0.25);
  ctx.fillStyle="#f4f4f4";ctx.fillRect(-16,-36,32,36);
  ctx.fillStyle="#1d4ed8";ctx.fillRect(-16,-22,32,14);
  ctx.fillStyle="#f2d06b";ctx.beginPath();ctx.arc(0,-44,10,0,Math.PI*2);ctx.fill();
  ctx.fillStyle="#111";ctx.fillRect(-22,-8,12,8);ctx.fillRect(10,-8,12,8);
  ctx.restore();
}
function drawBall(){
  ctx.beginPath();ctx.arc(ball.x,ball.y,9,0,Math.PI*2);
  ctx.fillStyle="#fff";ctx.fill();
  ctx.strokeStyle="#111";ctx.stroke();
}
function drawPlayer(){
  ctx.fillStyle="#8b1e2d";ctx.fillRect(W/2-14,H-58,28,34);
  ctx.fillStyle="#f2d06b";ctx.beginPath();ctx.arc(W/2,H-66,10,0,Math.PI*2);ctx.fill();
}
function drawBanner(){
  if(phase!=="over" && lastGoal==null) return;
  if(phase==="over"){
    ctx.fillStyle="rgba(7,20,34,0.72)";ctx.fillRect(0,0,W,H);
    ctx.fillStyle="#f2d06b";ctx.font="bold 42px sans-serif";ctx.textAlign="center";
    ctx.fillText(scoreP>scoreK?"冠军":"终场",W/2,H/2-10);
    ctx.fillStyle="#fff";ctx.font="18px sans-serif";
    ctx.fillText(scoreP+"  -  "+scoreK,W/2,H/2+28);
  }
}
function draw(){
  drawPitch();drawGoal();drawKeeper();drawPlayer();drawBall();drawBanner();
}
function toLocal(ev){
  var r=cv.getBoundingClientRect();
  return {x:(ev.clientX-r.left)*W/r.width,y:(ev.clientY-r.top)*H/r.height};
}
cv.addEventListener("click",function(ev){
  if(phase==="over")return;
  var p=toLocal(ev);shoot(p.x,p.y);
});
document.addEventListener("keydown",function(ev){
  if(ev.code==="Space"){ev.preventDefault();if(phase==="ready")shoot(W/2+(Math.random()-0.5)*200,120);}
});
document.getElementById("reset").onclick=function(){
  scoreP=scoreK=0;round=1;sudden=false;shotsP=shotsK=0;lastGoal=null;
  msg="点击球门射门";resetBall();setHud();
};
resetBall();setHud();tick();
})();
</script>
</body>
</html>
`
