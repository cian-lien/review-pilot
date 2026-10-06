import Ajv2020 from 'ajv/dist/2020.js';
import schema from '../schemas/task-v1.json';
import type { components } from './s1.gen';

export type TaskEnvelope = components['schemas']['TaskEnvelope'];
const validate = new Ajv2020({ strict: true, allErrors: false }).compile<TaskEnvelope>(schema);
export function validateTask(input: unknown): TaskEnvelope {
  if (!validate(input)) throw new Error('INVALID_TASK_MESSAGE');
  return input;
}
