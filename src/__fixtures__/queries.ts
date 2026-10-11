// Saved-query fixtures shared with the Go tests (pkg/backend/testdata/queries,
// see its README.md). Test-only: plugin code must never import this module.
import * as fs from 'fs';
import * as path from 'path';

export interface QueryFixture {
  name: string;
  source: string;
  /** Work package that makes the fixture pass; tests skip it while set. */
  pending?: string;
  description: string;
  query: Record<string, unknown> & { refId: string; queryType: string };
  expect: {
    params?: Record<string, string[]>;
    absentParams?: string[];
    sites?: string[];
    rows?: number;
    maxRows?: number;
    notice?: string;
    metrics?: string[];
  };
}

export const FIXTURE_DIR = path.resolve(__dirname, '../../pkg/backend/testdata/queries');

export function loadQueryFixtures(): QueryFixture[] {
  return fs
    .readdirSync(FIXTURE_DIR)
    .filter((file) => file.endsWith('.json'))
    .sort()
    .map((file) => JSON.parse(fs.readFileSync(path.join(FIXTURE_DIR, file), 'utf8')) as QueryFixture);
}
