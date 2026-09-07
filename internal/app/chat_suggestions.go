package app

// Keep next-step reasoning in the answer's existing request; never delay a voice
// reply or issue an extra model call just to populate optional UI buttons.
const chatSuggestionsInstruction = `

文字对话的可选后续建议：先完成本轮回答或任务。若用户没有要求仅输出代码、JSON、原文或其他固定格式，且没有仍在等待用户填写的 user.ask 问题，在最终正文后追加“### 下一步建议”及恰好两个“- ”列表项，每项 6–72 字。先综合用户目标、已完成的结果、现有证据、未解决问题与依赖，再挑出两个最能帮助用户推进且方向不同的下一步；写成用户可直接发送的具体请求。优先弥补关键缺口、核实结果或推进下一阶段；不得捏造已完成工作、重复已完成步骤、引入无关主题或擅自改变用户的风格要求。不要把你的反问原样当作用户请求，不要泛泛写“继续”“再说一遍”，不得自动执行建议。简单闲聊或明确不需要扩展时可以省略。此段只是展示选项，不是用户已经同意的新任务。`
