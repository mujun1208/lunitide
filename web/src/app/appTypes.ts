import type{ProjectDTO,SessionDTO,SkillDTO}from'../generated/bridge'
import type{ExecutionMode}from'../session/SessionPage'
import type{FilesFocus}from'../workspace/FilesPanel'

export type Page='home'|'projects'|'providers'|'settings'|'skill'|'expert'|'mcp'|'plugins'|'assets'|'automation'|'meetings'|'people'|'mro'
export type ChatTarget={project:ProjectDTO;session:SessionDTO;prompt?:string;noAutoSend?:boolean;providerId?:string;modelId?:string;executionMode?:ExecutionMode;composerTrigger?:'@'|'/'|'expert';initialUploadFiles?:File[];workspaceTab?:'files';workspacePath?:string;workspaceFocus?:FilesFocus;initialReferencedSkills?:SkillDTO[];companion?:boolean;personal:true}
export type ProjectTarget={project:ProjectDTO;session?:SessionDTO;prompt?:string;providerId?:string;modelId?:string;personal?:false}
export type LaunchTarget=ChatTarget|ProjectTarget
export type Theme='dark'|'light'