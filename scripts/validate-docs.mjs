#!/usr/bin/env node

import assert from 'node:assert/strict';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { basename, dirname, extname, join, relative, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { TextDecoder } from 'node:util';
import { fileURLToPath } from 'node:url';

const CJK_PATTERN = /[\u3400-\u4dbf\u4e00-\u9fff]/u;
const REPLACEMENT_CHARACTER = '\uFFFD';
const PLACEHOLDER_PATTERN = /\b(?:TODO|TBD|FIXME|PLACEHOLDER)\b|待定|待补充/iu;
const SECRET_PATTERNS = [
  { label: 'OpenAI 风格密钥', pattern: /\bsk-[A-Za-z0-9_-]{20,}\b/u },
  { label: '本地访问密钥', pattern: /\bah_local_[A-Za-z0-9_-]{24,}\b/u },
  { label: 'GitHub 访问令牌', pattern: /\bgh[pousr]_[A-Za-z0-9_]{20,}\b/u },
  { label: 'Slack 访问令牌', pattern: /\bxox[baprs]-[A-Za-z0-9-]{20,}\b/u },
];
const OPEN_FENCE_PATTERN = /^\s{0,3}(`{3,}|~{3,})/u;
const CLOSE_FENCE_PATTERN = /^\s{0,3}(`{3,}|~{3,})\s*$/u;
const MARKDOWN_LINK_PATTERN = /!?\[[^\]\n]*\]\(\s*(?:<([^>\n]+)>|([^\s)\n]+))[^)\n]*\)/gu;

function displayPath(filePath, docsRoot) {
  const value = relative(docsRoot, filePath);
  return value ? value.replaceAll('\\', '/') : basename(filePath);
}

function addError(errors, filePath, docsRoot, message) {
  errors.push(`${displayPath(filePath, docsRoot)}：${message}`);
}

function readUtf8(filePath) {
  const bytes = readFileSync(filePath);
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
}

function findMarkdownFiles(root) {
  const files = [];
  const entries = readdirSync(root, { withFileTypes: true }).sort((left, right) => left.name.localeCompare(right.name));

  for (const entry of entries) {
    const entryPath = join(root, entry.name);
    if (entry.isDirectory()) {
      files.push(...findMarkdownFiles(entryPath));
      continue;
    }
    if (entry.isFile() && extname(entry.name).toLowerCase() === '.md') {
      files.push(entryPath);
    }
  }

  return files;
}

function checkCodeFences(text, filePath, docsRoot, errors) {
  const lines = text.split('\n');
  let activeFence = null;

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    if (activeFence) {
      const closing = line.match(CLOSE_FENCE_PATTERN);
      if (closing && closing[1][0] === activeFence.character && closing[1].length >= activeFence.length) {
        activeFence = null;
      }
      continue;
    }

    const opening = line.match(OPEN_FENCE_PATTERN);
    if (opening) {
      activeFence = {
        character: opening[1][0],
        length: opening[1].length,
        line: index + 1,
      };
    }
  }

  if (activeFence) {
    addError(errors, filePath, docsRoot, `代码围栏未闭合，起始于第 ${activeFence.line} 行`);
  }
}

function blankMarkdownCode(text) {
  const lines = text.split('\n');
  const output = [];
  let activeFence = null;

  for (const line of lines) {
    if (activeFence) {
      const closing = line.match(CLOSE_FENCE_PATTERN);
      output.push('');
      if (closing && closing[1][0] === activeFence.character && closing[1].length >= activeFence.length) {
        activeFence = null;
      }
      continue;
    }

    const opening = line.match(OPEN_FENCE_PATTERN);
    if (opening) {
      output.push('');
      activeFence = { character: opening[1][0], length: opening[1].length };
      continue;
    }

    output.push(line.replace(/(`+)([\s\S]*?)\1/gu, (match) => match.replace(/[^\n]/gu, ' ')));
  }

  return output.join('\n');
}

function checkInternalLinks(text, filePath, docsRoot, errors) {
  const scanText = blankMarkdownCode(text);

  for (const match of scanText.matchAll(MARKDOWN_LINK_PATTERN)) {
    const rawTarget = (match[1] ?? match[2] ?? '').trim();
    if (!rawTarget || rawTarget.startsWith('#') || /^(?:https?:|mailto:)/iu.test(rawTarget)) {
      continue;
    }

    let target;
    try {
      target = decodeURIComponent(rawTarget);
    } catch {
      addError(errors, filePath, docsRoot, `内部链接 URL 编码无效：${rawTarget}`);
      continue;
    }

    const targetWithoutFragment = target.split(/[?#]/u, 1)[0];
    if (!targetWithoutFragment) {
      continue;
    }

    const targetPath = resolve(dirname(filePath), targetWithoutFragment);
    if (!existsSync(targetPath)) {
      addError(errors, filePath, docsRoot, `内部链接不存在：${rawTarget}`);
    }
  }
}

function checkDocument(filePath, docsRoot, errors) {
  let text;
  try {
    text = readUtf8(filePath);
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    addError(errors, filePath, docsRoot, `不是可读取的有效 UTF-8 文本（${detail}）`);
    return;
  }

  if (text.includes(REPLACEMENT_CHARACTER)) {
    addError(errors, filePath, docsRoot, `包含 Unicode replacement character（${REPLACEMENT_CHARACTER}）`);
  }
  if (!CJK_PATTERN.test(text)) {
    addError(errors, filePath, docsRoot, '缺少 CJK 文档内容');
  }
  if (PLACEHOLDER_PATTERN.test(text)) {
    addError(errors, filePath, docsRoot, '包含未完成标记（TODO/TBD/FIXME/PLACEHOLDER/待定/待补充）');
  }
  for (const secret of SECRET_PATTERNS) {
    if (secret.pattern.test(text)) {
      addError(errors, filePath, docsRoot, `疑似 live secret：${secret.label}`);
    }
  }

  checkCodeFences(text, filePath, docsRoot, errors);
  checkInternalLinks(text, filePath, docsRoot, errors);
}

function validateDocs(docsRoot) {
  const errors = [];
  const root = resolve(docsRoot);

  if (!existsSync(root) || !statSync(root).isDirectory()) {
    errors.push(`docs 根目录不存在或不可读取：${root}`);
    return { files: [], errors };
  }

  let files;
  try {
    files = findMarkdownFiles(root);
  } catch (error) {
    const detail = error instanceof Error ? error.message : String(error);
    errors.push(`递归读取 docs 目录失败：${detail}`);
    return { files: [], errors };
  }

  if (files.length === 0) {
    errors.push('docs 目录下没有 Markdown 文件');
    return { files, errors };
  }

  for (const filePath of files) {
    checkDocument(filePath, root, errors);
  }

  return { files, errors };
}

function expectValidationFailure(root, fileName, content, expectedMessage) {
  writeFileSync(join(root, fileName), content, 'utf8');
  const result = validateDocs(root);
  assert.ok(
    result.errors.some((error) => error.includes(expectedMessage)),
    `self-test 未捕获预期错误：${expectedMessage}`,
  );
}

function runSelfTest() {
  const root = mkdtempSync(join(tmpdir(), 'aggregation-hub-docs-self-test-'));
  try {
    const validRoot = join(root, 'valid');
    const validTarget = join(validRoot, 'target.md');
    const validDocument = join(validRoot, 'valid.md');
    const invalidRoot = join(root, 'invalid');
    const invalidLinkRoot = join(invalidRoot, 'link');
    const invalidFenceRoot = join(invalidRoot, 'fence');
    const invalidMarkerRoot = join(invalidRoot, 'marker');
    const invalidSecretRoot = join(invalidRoot, 'secret');

    for (const directory of [validRoot, invalidLinkRoot, invalidFenceRoot, invalidMarkerRoot, invalidSecretRoot]) {
      mkdirForSelfTest(directory);
    }

    writeFileSync(validTarget, '# 目标文档\n', 'utf8');
    writeFileSync(
      validDocument,
      '# 合法文档\n\n[目标文档](./target.md#章节)\n\n行内代码：`[不会被当成链接](missing.md)`\n\n```ts\nconst sample = "[代码块链接](missing.md)";\n```\n',
      'utf8',
    );
    assert.deepEqual(validateDocs(validRoot).errors, [], 'self-test 合法文档不应失败');

    expectValidationFailure(invalidLinkRoot, 'bad-link.md', '# 失效链接\n[不存在](./missing.md)\n', '内部链接不存在');
    expectValidationFailure(invalidFenceRoot, 'bad-fence.md', '# 未闭合围栏\n```ts\nconst value = 1;\n', '代码围栏未闭合');
    expectValidationFailure(invalidMarkerRoot, 'bad-marker.md', '# 未完成标记\n这里有 TODO 需要处理。\n', '包含未完成标记');
    expectValidationFailure(
      invalidSecretRoot,
      'bad-secret.md',
      `# 疑似密钥\nsk-${'a'.repeat(24)}\nah_local_${'b'.repeat(28)}\n`,
      '疑似 live secret',
    );

    console.log('文档校验 self-test 通过：坏链接、坏围栏、未完成标记和疑似 Secret 均被拒绝；代码片段链接未误报。');
    return 0;
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

function mkdirForSelfTest(directory) {
  mkdirSync(directory, { recursive: true });
}

function main() {
  const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
  const docsRoot = resolve(projectRoot, 'docs');

  if (process.argv.includes('--self-test')) {
    return runSelfTest();
  }

  const result = validateDocs(docsRoot);
  if (result.errors.length > 0) {
    console.error(`文档校验失败：发现 ${result.errors.length} 个问题。`);
    for (const error of result.errors) {
      console.error(`- ${error}`);
    }
    return 1;
  }

  console.log(`文档校验通过：递归检查 ${result.files.length} 个 Markdown 文件。`);
  return 0;
}

try {
  process.exitCode = main();
} catch (error) {
  const detail = error instanceof Error ? error.message : String(error);
  console.error(`文档校验异常：${detail}`);
  process.exitCode = 1;
}