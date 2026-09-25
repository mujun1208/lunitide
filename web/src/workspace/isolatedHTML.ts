/** Preview documents without scripts, network access, nested frames or forms. */
export const isolatedHTML = (content: string) =>
  `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'; font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">${content}`

/** A generated page with no preview ticket still runs here. Scripts and buttons
 *  work. The network, nested frames, and this application's origin stay closed. */
const storageShim = `<script>(function(){var mem={};var api={getItem:function(k){return Object.prototype.hasOwnProperty.call(mem,k)?mem[k]:null},setItem:function(k,v){mem[String(k)]=String(v)},removeItem:function(k){delete mem[k]},clear:function(){mem={}},key:function(i){return Object.keys(mem)[i]||null},get length(){return Object.keys(mem).length}};var blocked=false;try{void window.localStorage}catch(e){blocked=true}if(!blocked)return;try{Object.defineProperty(window,'localStorage',{configurable:true,get:function(){return api}})}catch(e){}try{Object.defineProperty(window,'sessionStorage',{configurable:true,get:function(){return api}})}catch(e){}})()</script>`

export const runnableHTML = (content: string) =>
  `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">${storageShim}${content}`
