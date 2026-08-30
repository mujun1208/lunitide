package htmlapp

import "strings"

const checklistFallbackTitle = "清单"

func renderChecklist(title string) string {
	safe := safeTitle(title, checklistFallbackTitle)
	page := strings.ReplaceAll(checklistHTML, "{{TITLE}}", safe)
	return strings.ReplaceAll(page, "{{STORAGE}}", "lunitide-checklist-"+safe)
}

const checklistHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{TITLE}}</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
html,body{height:100%;background:#10141c;color:#eef3ff;font-family:"Segoe UI","Microsoft YaHei",sans-serif}
#wrap{min-height:100%;max-width:560px;margin:0 auto;padding:28px 20px 40px;display:flex;flex-direction:column;gap:16px}
h1{font-size:22px;letter-spacing:1px}
.row{display:flex;gap:8px}
input,button{padding:8px 14px;border:0;border-radius:8px;font:inherit}
input{flex:1;background:#1b2230;color:#eef3ff}
button{background:#3fd6ff;color:#071018;font-weight:700;cursor:pointer}
#list{list-style:none;display:grid;gap:8px}
#list li{display:flex;align-items:center;gap:10px;padding:10px 12px;border-radius:8px;background:#1b2230}
#list li.done span{text-decoration:line-through;opacity:.55}
#list button{background:#2a3344;color:#eef3ff;font-weight:600}
</style>
</head>
<body>
<div id="wrap">
<h1>{{TITLE}}</h1>
<div class="row">
<input id="item" maxlength="200" placeholder="添加一项" aria-label="新待办">
<button id="add" type="button">添加</button>
<button id="clear" type="button">清除已完成</button>
</div>
<ul id="list"></ul>
</div>
<script>
(function(){
var key="{{STORAGE}}";
var list=document.getElementById("list");
var box=document.getElementById("item");
function load(){try{return JSON.parse(localStorage.getItem(key)||"[]")}catch(e){return []}}
function save(items){localStorage.setItem(key,JSON.stringify(items))}
function draw(){
  var items=load();
  list.innerHTML="";
  items.forEach(function(it,i){
    var li=document.createElement("li");
    if(it.done)li.className="done";
    var ck=document.createElement("input");
    ck.type="checkbox";ck.checked=!!it.done;ck.setAttribute("aria-label","完成");
    ck.onchange=function(){items[i].done=ck.checked;save(items);draw()};
    var span=document.createElement("span");span.textContent=it.text;
    var del=document.createElement("button");del.type="button";del.textContent="删除";
    del.onclick=function(){items.splice(i,1);save(items);draw()};
    li.appendChild(ck);li.appendChild(span);li.appendChild(del);list.appendChild(li);
  });
}
document.getElementById("add").onclick=function(){
  var text=box.value.replace(/^\s+|\s+$/g,"");
  if(!text)return;
  var items=load();items.push({text:text,done:false});save(items);box.value="";draw();
};
document.getElementById("clear").onclick=function(){
  save(load().filter(function(it){return !it.done}));draw();
};
box.addEventListener("keydown",function(e){if(e.key==="Enter")document.getElementById("add").click()});
draw();
})();
</script>
</body>
</html>
`
