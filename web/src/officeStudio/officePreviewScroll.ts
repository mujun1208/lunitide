export function scrollOfficeNodeIntoView(root: Element | null, node: Element | null): boolean {
  if (!(root instanceof HTMLElement) || !(node instanceof HTMLElement) || !root.contains(node)) return false
  const top = node.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop - 12
  root.scrollTop = Math.max(0, top)
  return true
}
