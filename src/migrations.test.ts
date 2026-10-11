import { migrateQuery, toStringList } from './migrations';
import { loadQueryFixtures } from './__fixtures__/queries';
import type { CatalystQuery } from './types';

const fixtures = Object.fromEntries(loadQueryFixtures().map((f) => [f.name, f.query as unknown as CatalystQuery]));
const DEVICE_UUID = '00000000-0000-4000-8000-000000000101';

describe('toStringList', () => {
  it.each([
    [undefined, []],
    [null, []],
    ['', []],
    ['  ', []],
    ['site-1', ['site-1']],
    [
      ['a', '', ' b '],
      ['a', 'b'],
    ],
    [[1, 'a'], ['a']],
  ])('%p -> %p', (input, expected) => {
    expect(toStringList(input)).toEqual(expected);
  });
});

describe('migrateQuery', () => {
  it('turns a v1.0.x string siteId into a list', () => {
    expect(migrateQuery(fixtures['v1x-site-string']).siteId).toEqual(['00000000-0000-4000-8000-000000000004']);
  });

  it('turns empty site strings into empty lists', () => {
    const q = migrateQuery(fixtures['provisioned-empty-site-strings']);
    expect(q.siteId).toEqual([]);
    expect(q.siteName).toEqual([]);
  });

  it('keeps missing fields missing', () => {
    const q = migrateQuery(fixtures['v1x-no-filters']);
    expect(q).not.toHaveProperty('siteId');
    expect(q).not.toHaveProperty('status');
  });

  it('maps issueStatus to status and severity to priority', () => {
    const q = migrateQuery(fixtures['v1x-priority-status']);
    expect(q.status).toEqual(['ACTIVE']);
    expect(q).not.toHaveProperty('issueStatus');

    const alias = migrateQuery(fixtures['handedited-status-severity-alias']);
    expect(alias.status).toEqual(['RESOLVED']);
    expect(alias.priority).toEqual(['P3']);
    expect(alias).not.toHaveProperty('severity');
  });

  it('does not overwrite current fields with legacy ones', () => {
    const q = migrateQuery({ refId: 'A', queryType: 'alerts', status: ['RESOLVED'], issueStatus: 'ACTIVE' } as any);
    expect(q.status).toEqual(['RESOLVED']);
  });

  it('maps a UUID deviceId to networkDeviceId and keeps other values for the backend', () => {
    const uuid = migrateQuery(fixtures['v1x-device-uuid']);
    expect(uuid.networkDeviceId).toBe(DEVICE_UUID);
    expect(uuid).not.toHaveProperty('deviceId');

    expect(migrateQuery(fixtures['v1x-device-ip']).deviceId).toBe('10.10.20.5');
  });

  it("converts aiDriven 'true' and keeps the explicit 'false'", () => {
    expect(migrateQuery(fixtures['v1x-aidriven-true']).aiDriven).toBe(true);
    expect(migrateQuery(fixtures['v1x-aidriven-false']).aiDriven).toBe('false');
  });

  it('does not modify its input', () => {
    const input = fixtures['v1x-site-string'];
    const before = JSON.stringify(input);
    migrateQuery(input);
    expect(JSON.stringify(input)).toBe(before);
  });

  it.each(Object.keys(fixtures))('%s: list fields are arrays or absent', (name) => {
    const q = migrateQuery(fixtures[name]) as unknown as Record<string, unknown>;
    for (const key of ['siteId', 'siteName', 'priority', 'status', 'metrics']) {
      expect(q[key] === undefined || Array.isArray(q[key])).toBe(true);
    }
  });
});
