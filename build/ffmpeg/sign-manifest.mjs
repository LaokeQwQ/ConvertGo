#!/usr/bin/env node
import { createHash, createPrivateKey, createPublicKey, sign } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { readFile, readdir, stat, writeFile } from 'node:fs/promises';
import { basename, join } from 'node:path';

const [directory, version, repository] = process.argv.slice(2);
if (!directory || !/^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(version ?? '') ||
    !/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository ?? '')) {
  throw new Error('usage: sign-manifest.mjs ASSET_DIRECTORY vX.Y.Z OWNER/REPOSITORY');
}
const secret = process.env.CONVERTGO_UPDATE_SIGNING_KEY;
if (!secret) throw new Error('CONVERTGO_UPDATE_SIGNING_KEY is missing; refusing unsigned release');
// Keep the key entirely in process memory, never in a file or diagnostic output.
let key;
try {
  if (secret.includes('-----BEGIN PRIVATE KEY-----')) {
    key = createPrivateKey(secret.replaceAll('\\n', '\n'));
  } else {
    const bytes = Buffer.from(secret.trim(), 'base64');
    if (bytes.length !== 32 && bytes.length !== 64) throw new Error('bad key length');
    const pkcs8 = Buffer.concat([Buffer.from('302e020100300506032b657004220420', 'hex'), bytes.subarray(0, 32)]);
    key = createPrivateKey({ key: pkcs8, format: 'der', type: 'pkcs8' });
    bytes.fill(0);
    pkcs8.fill(0);
  }
} catch {
  throw new Error('signing key must be Ed25519 PKCS#8 PEM or base64 32-byte seed/64-byte private key');
}
delete process.env.CONVERTGO_UPDATE_SIGNING_KEY;
if (key.asymmetricKeyType !== 'ed25519') throw new Error('signing key is not Ed25519');
const expectedPublicKey = Buffer.from('MCowBQYDK2VwAyEAbG1swJYV/cc66ecIg+mAb6FP3D+eSSdHnvknMm3uBU0=', 'base64');
if (!createPublicKey(key).export({ format: 'der', type: 'spki' }).equals(expectedPublicKey)) {
  throw new Error('signing key does not match the production updater public key');
}
const entries = [];
for (const name of (await readdir(directory)).filter(name => name.endsWith('.asset.json'))) {
  const record = JSON.parse(await readFile(join(directory, name), 'utf8'));
  const { os, arch, filename } = record;
  if (!['darwin', 'windows', 'linux'].includes(os) || !['386', 'amd64', 'arm64'].includes(arch) ||
      basename(filename ?? '') !== filename || filename !== `ConvertGo-${version}-${os}-${arch}${os === 'darwin' ? '.dmg' : os === 'windows' ? '-setup.exe' : '.tar.gz'}`) {
    throw new Error('invalid release asset record');
  }
  const file = join(directory, filename);
  const size = (await stat(file)).size;
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(file)) hash.update(chunk);
  const sha256 = hash.digest('hex');
  if (sha256 !== record.sha256 || size !== record.size) throw new Error('release asset bytes changed after packaging');
  entries.push({ os, arch, filename, sha256, size });
}
entries.sort((a, b) => {
  for (const field of ['os', 'arch', 'filename']) {
    if (a[field] < b[field]) return -1;
    if (a[field] > b[field]) return 1;
  }
  return 0;
});
const expectedTargets = new Set(['darwin-amd64', 'darwin-arm64', 'windows-386', 'windows-amd64', 'windows-arm64', 'linux-386', 'linux-amd64', 'linux-arm64']);
for (const entry of entries) {
  if (!expectedTargets.delete(`${entry.os}-${entry.arch}`)) throw new Error('duplicate or unsupported release target');
}
if (expectedTargets.size) throw new Error('missing release targets: ' + [...expectedTargets].join(', '));
// Matches Go struct field order with SetEscapeHTML(false), without a trailing newline.
const unsigned = JSON.stringify({ version, assets: entries });
const signature = sign(null, Buffer.from(unsigned, 'utf8'), key).toString('base64');
await writeFile(join(directory, 'update-manifest.json'), JSON.stringify({ version, assets: entries, signature }) + '\n');
await writeFile(join(directory, 'SHA256SUMS'), entries.map(entry => `${entry.sha256}  ${entry.filename}\n`).join(''));
