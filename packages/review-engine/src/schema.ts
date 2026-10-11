import source from '../../contracts/schemas/review-api.gen.json';
export function modelSchema(
  name: 'ReviewCandidates' | 'ReviewVerification',
): Record<string, unknown> {
  const definitions = source.components.schemas as Record<string, unknown>;
  function resolve(value: unknown): unknown {
    if (Array.isArray(value)) return value.map(resolve);
    if (!value || typeof value !== 'object') return value;
    const record = value as Record<string, unknown>;
    if (typeof record.$ref === 'string')
      return resolve(definitions[record.$ref.split('/').at(-1)!]);
    return Object.fromEntries(
      Object.entries(record)
        .filter(([key]) => !key.startsWith('x-') && key !== 'format')
        .map(([key, child]) => [key, resolve(child)]),
    );
  }
  return resolve(definitions[name]) as Record<string, unknown>;
}
