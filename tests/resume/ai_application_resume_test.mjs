import assert from 'node:assert/strict';
import { access, readFile } from 'node:fs/promises';
import { constants } from 'node:fs';

const htmlPath = new URL(
  '../../artifacts/resume/lan-songsong-ai-application-engineer.html',
  import.meta.url,
);
const photoPath = new URL(
  '../../artifacts/resume/assets/lan-songsong-photo.jpg',
  import.meta.url,
);
const html = await readFile(htmlPath, 'utf8');

for (const text of [
  'AI 应用工程师',
  '2027 届',
  '深圳大学',
  'Supervisor',
  '5 个领域 Agent',
  '20 个业务 Tool',
  '2 个记忆能力 Tool',
  'MemoryProvider',
  'StatefulInterrupt',
  'MCP',
  'Agent 评估',
  '800ms',
  '350ms',
  '40%',
]) {
  assert.ok(html.includes(text), `missing required text: ${text}`);
}

for (const forbidden of ['待补充', '待确认', '虚构示例照片']) {
  assert.ok(!html.includes(forbidden), `contains placeholder: ${forbidden}`);
}

assert.match(html, /@page\s*\{[^}]*size:\s*A4/i);
assert.match(html, /@media\s+print/i);
assert.match(html, /\.toolbar[^}]*display:\s*none/i);
assert.match(html, /contenteditable="true"/i);
assert.match(html, /assets\/lan-songsong-photo\.jpg/);
await access(photoPath, constants.R_OK);

console.log('resume HTML static checks passed');
