import React,{useMemo}from'react'
import{Aurora}from'../session/companion/visual/Aurora'
import{canUseCompanionWebgl}from'../session/companion/visual/webglSupport'
import{launchAuroraProps}from'../session/companion/visual/moonVisual'
import{seededUnit}from'./appHelpers'
import type{Theme}from'./appTypes'

export function Atmosphere({theme,aurora=false}:{theme:Theme;aurora?:boolean}):React.JSX.Element{
 const stars=useMemo(()=>Array.from({length:110},(_,i)=>({left:seededUnit(i,1)*100,top:seededUnit(i,2)*100,size:.45+seededUnit(i,3)*2.25,delay:seededUnit(i,4)*-7,duration:3.2+seededUnit(i,5)*5,bright:seededUnit(i,6)>.88})),[])
 const webgl=aurora&&canUseCompanionWebgl()
 return <div className="atmosphere" data-aurora={webgl?'webgl':undefined} aria-hidden="true"><div className="sky"/>{webgl&&<div className="atmosphere-aurora"><Aurora {...launchAuroraProps(theme)}/></div>}<div className="nebula"/><div className="moon-glow"/><div className="moon ambient-moon"><i/><b/><span/></div><div className="stars">{stars.map((s,i)=><i key={i} className={s.bright?'bright':''} style={{left:`${s.left}%`,top:`${s.top}%`,width:`${s.size}px`,height:`${s.size}px`,animationDelay:`${s.delay}s`,animationDuration:`${s.duration}s`}}/>)}</div><div className="vignette"/></div>
}
export function Moon({small=false}:{small?:boolean}):React.JSX.Element{return <span className={`real-moon ${small?'small':''}`} aria-hidden="true"><i/><b/><em/></span>}