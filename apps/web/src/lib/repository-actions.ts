import type { APIClient, SetEnabledInput } from './api-client';
export function toggleRepository(client: APIClient, input: SetEnabledInput): Promise<void> {
  return client.setEnabled(input);
}
