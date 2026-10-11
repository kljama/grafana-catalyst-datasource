import { loadQueryFixtures } from './queries';

describe('saved-query fixtures', () => {
  const fixtures = loadQueryFixtures();

  it('loads the shared fixture files', () => {
    expect(fixtures.length).toBeGreaterThan(0);
    expect(new Set(fixtures.map((f) => f.name)).size).toBe(fixtures.length);
  });

  it.each(fixtures.map((f) => [f.name, f] as const))('%s is well-formed', (_name, fixture) => {
    expect(fixture.source).toBeTruthy();
    expect(fixture.description).toBeTruthy();
    expect(fixture.query.refId).toBeTruthy();
    expect(fixture.query.queryType).toBeTruthy();
    expect(Object.keys(fixture.expect).length).toBeGreaterThan(0);
  });

  it('covers the legacy v1.0.x shapes the frontend must migrate', () => {
    const byName = Object.fromEntries(fixtures.map((f) => [f.name, f]));
    expect(typeof byName['v1x-site-string'].query.siteId).toBe('string');
    expect(byName['v1x-priority-status'].query.issueStatus).toBe('ACTIVE');
    expect(byName['v1x-aidriven-false'].query.aiDriven).toBe('false');
    expect(byName['v1x-no-filters'].query).not.toHaveProperty('siteId');
  });
});
