export function findInstalledExpert<T extends {expertId: string; catalogItemId?: string; name?: string; state?: string}>(
  items: readonly T[],
  catalogItemId: string,
): T | undefined {
  const key = catalogItemId.trim()
  return items.find(item => (item.catalogItemId ?? '') === key)
}

export function isEnabledMroExpert(items: readonly {catalogItemId?: string; state?: string}[]): boolean {
  return items.some(item => item.catalogItemId === 'mro-expert' && item.state === 'enabled')
}
