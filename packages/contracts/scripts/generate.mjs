import { readFile, writeFile, mkdtemp, rm, mkdir } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import YAML from 'yaml';
import openapiTS, { astToString } from 'openapi-typescript';
import { reviewComponents, reviewPaths } from './review-components.mjs';

const root = new URL('../', import.meta.url);
const goRoot = fileURLToPath(new URL('../../../apps/api/', import.meta.url));
const schema = JSON.parse(await readFile(new URL('schemas/task-v1.json', root), 'utf8'));
const reviewSchema = JSON.parse(await readFile(new URL('schemas/task-v2.json', root), 'utf8'));
const specPath = new URL('openapi/s1.yaml', root);
const spec = YAML.parse(await readFile(specPath, 'utf8'));
const project = (value) => {
  if (Array.isArray(value)) return value.map(project);
  if (value === null || typeof value !== 'object') return value;
  const output = {};
  for (const [key, item] of Object.entries(value)) {
    if (['$schema', '$id', '$defs'].includes(key)) continue;
    if (key === 'const') output.enum = [item];
    else if (key === '$ref') output.$ref = item.replace('#/$defs/', '#/components/schemas/');
    else output[key] = project(item);
  }
  return output;
};
const projected = project(schema);
const check = process.argv.includes('--check');
if (check && JSON.stringify(spec.components.schemas.TaskEnvelope) !== JSON.stringify(projected)) {
  throw new Error('TaskEnvelope projection drift: run codegen');
}
spec.components.schemas.TaskEnvelope = projected;
if (
  check &&
  JSON.stringify(spec.components.schemas.ReviewTaskEnvelope) !==
    JSON.stringify(project(reviewSchema))
)
  throw new Error('ReviewTaskEnvelope projection drift');
spec.components.schemas.ReviewTaskEnvelope = project(reviewSchema);
for (const [name, value] of Object.entries(reviewComponents)) {
  if (check && JSON.stringify(spec.components.schemas[name]) !== JSON.stringify(value))
    throw new Error(name + ' review projection drift');
  spec.components.schemas[name] = value;
}
for (const [name, value] of Object.entries(reviewPaths)) {
  if (check && JSON.stringify(spec.paths[name]) !== JSON.stringify(value))
    throw new Error(name + ' path drift');
  spec.paths[name] = value;
}
for (const [name, value] of Object.entries(schema.$defs)) {
  if (check && JSON.stringify(spec.components.schemas[name]) !== JSON.stringify(project(value)))
    throw new Error(name + ' projection drift');
  spec.components.schemas[name] = project(value);
}
const source = YAML.stringify(spec);
const ts = astToString(await openapiTS(spec));
const dir = await mkdtemp(join(tmpdir(), 'reviewpilot-codegen-'));
try {
  const specFile = join(dir, 's1.yaml');
  const goFile = join(dir, 's1.gen.go');
  await writeFile(specFile, source);
  const result = spawnSync(
    'go',
    [
      'run',
      'github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0',
      '-generate',
      'types,skip-prune',
      '-package',
      'contracts',
      '-o',
      goFile,
      specFile,
    ],
    { cwd: goRoot, encoding: 'utf8', maxBuffer: 10 * 1024 * 1024 },
  );
  if (result.status !== 0) throw new Error(result.stderr || 'Go codegen failed');
  const generated = [
    [
      new URL(
        'internal/contracts/review-api.gen.json',
        new URL('../../../apps/api/', import.meta.url),
      ),
      JSON.stringify({ components: spec.components }, null, 2) + '\n',
    ],
    [
      new URL('schemas/review-api.gen.json', root),
      JSON.stringify({ components: spec.components }, null, 2) + '\n',
    ],
    [new URL('src/s1.gen.ts', root), ts],
    [
      new URL('internal/contracts/s1.gen.go', new URL('../../../apps/api/', import.meta.url)),
      await readFile(goFile, 'utf8'),
    ],
  ];
  for (const [path, content] of generated) {
    if (check) {
      if ((await readFile(path, 'utf8')) !== content)
        throw new Error('Generated contract drift: ' + fileURLToPath(path));
    } else {
      await mkdir(new URL('.', path), { recursive: true });
      await writeFile(path, content);
    }
  }
  if (!check) await writeFile(specPath, source);
} finally {
  await rm(dir, { recursive: true, force: true });
}
