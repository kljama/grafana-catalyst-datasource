// legacyQueries.test.tsx: saved queries of older versions through the real
// DataSource and QueryEditor (review issues C2 and C3).
import React from 'react';
import { render, screen } from '@testing-library/react';
import type { DataSourceInstanceSettings } from '@grafana/data';
import { getTemplateSrv } from '@grafana/runtime';
import { DataSource } from './datasource';
import QueryEditor from './components/QueryEditor';
import { loadQueryFixtures } from './__fixtures__/queries';
import type { CatalystJsonData, CatalystQuery } from './types';

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: jest.fn(),
}));

// Like Grafana's TemplateSrv.replace: a missing or empty target becomes ''.
const grafanaLikeReplace = (target?: string) => (target ? target : target ?? '');

const fixtures = loadQueryFixtures().filter((f) => f.query.queryType !== 'siteHealth');

function newDataSource() {
  return new DataSource({
    id: 1,
    uid: 'catalyst',
    type: 'grafana-catalyst-datasource',
    name: 'Catalyst',
    meta: {} as any,
    jsonData: { baseUrl: 'https://catalyst.example.com' },
    access: 'proxy',
    readOnly: false,
  } as DataSourceInstanceSettings<CatalystJsonData>);
}

function renderEditor(query: CatalystQuery) {
  const datasource = { getResource: jest.fn().mockResolvedValue([]) } as any;
  return render(
    <QueryEditor query={query} onChange={jest.fn()} onRunQuery={jest.fn()} datasource={datasource} queries={[]} />
  );
}

beforeEach(() => {
  (getTemplateSrv as jest.Mock).mockReturnValue({ replace: jest.fn(grafanaLikeReplace) });
});

describe('saved queries of older versions', () => {
  it.each(fixtures.map((f) => [f.name, f.query] as const))(
    '%s: applyTemplateVariables sends lists the backend accepts',
    (_name, saved) => {
      const out = newDataSource().applyTemplateVariables(saved as unknown as CatalystQuery, {});
      for (const key of ['siteId', 'siteName'] as const) {
        const value = out[key];
        expect(value === undefined || (Array.isArray(value) && value.every((v) => v !== ''))).toBe(true);
      }
    }
  );

  it.each(fixtures.map((f) => [f.name, f.query] as const))('%s: Explore link renders the editor', (_name, saved) => {
    // Grafana builds Explore links through interpolateVariablesInQueries.
    const [explored] = newDataSource().interpolateVariablesInQueries([saved as unknown as CatalystQuery], {});
    expect(() => renderEditor(explored)).not.toThrow();
  });

  it('renders a query whose siteId is a string or an empty string', () => {
    expect(() => renderEditor({ refId: 'A', queryType: 'alerts', siteId: 'uuid-1' as any })).not.toThrow();
    expect(() => renderEditor({ refId: 'B', queryType: 'alerts', siteId: '' as any })).not.toThrow();
  });

  it("shows aiDriven 'false' as switch off", () => {
    renderEditor({ refId: 'A', queryType: 'alerts', aiDriven: 'false' });
    const toggle = screen.getAllByRole('switch')[0] as HTMLInputElement;
    expect(toggle.checked).toBe(false);
  });
});
