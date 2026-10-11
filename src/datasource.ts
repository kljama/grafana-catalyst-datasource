// This file implements the main frontend logic for the Catalyst datasource plugin.
// It extends DataSourceWithBackend, which means most query logic is handled by the Go backend.
// This class is responsible for:
// - Providing default query values for new panels
// - Filtering out invalid queries before sending to the backend
// - Implementing template variable queries (metricFindQuery)
// - Efficiently extracting unique values for template variables from recent issues

import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';
import type {
  CoreApp,
  DataSourceInstanceSettings,
  MetricFindValue,
  ScopedVars,
} from '@grafana/data';
import {
  DEFAULT_QUERY as DEFAULTS,
  type CatalystQuery,
  type CatalystJsonData,
  type CatalystVariableQuery,
} from './types';
import { parseError } from './errors';
import { migrateQuery } from './migrations';
import { logger } from './logger';

type InstanceSettings = DataSourceInstanceSettings<CatalystJsonData>;

// PAGE_SIZE: Number of rows to fetch per page when populating template variables.
// The assuranceIssues API accepts at most 25 per request, and its offset starts at 1.
// MAX_PAGES: Maximum number of pages to fetch (prevents excessive API calls).
const PAGE_SIZE = 25;
const MAX_PAGES = 5; // variable helper only

export class DataSource extends DataSourceWithBackend<CatalystQuery, CatalystJsonData> {
  instanceSettings: InstanceSettings;
  private log = logger.child('DataSource');

  constructor(instanceSettings: InstanceSettings) {
    super(instanceSettings);
    this.instanceSettings = instanceSettings;
    this.log.debug('DataSource initialized', { uid: instanceSettings.uid });
  }

  // Returns the default query structure for new panels/targets.
  getDefaultQuery(app: CoreApp): Partial<CatalystQuery> {
    // Use endpoint from config if available
    const endpoint = (this.instanceSettings.jsonData?.endpoint ?? 'alerts') as CatalystQuery['endpoint'];
    return { ...DEFAULTS, endpoint, queryType: endpoint === 'siteHealth' ? 'siteHealth' : 'alerts' };
  }

  // filterQuery is called by Grafana to prevent the execution of empty or invalid queries.
  // We allow all queries to pass through, as the backend handles validation.
  filterQuery(query: CatalystQuery): boolean {
    return true;
  }

  // applyTemplateVariables is a required method that Grafana uses to substitute
  // template variables into a query before sending it to the backend. It also runs
  // for Explore links (interpolateVariablesInQueries).
  //
  // Missing fields must stay undefined: templateSrv.replace(undefined) returns '',
  // and the backend expects lists for siteId/siteName.
  applyTemplateVariables(query: CatalystQuery, scopedVars: ScopedVars): CatalystQuery {
    const templateSrv = getTemplateSrv();
    const q = migrateQuery(query);
    const replaceString = (val: string | undefined) =>
      val === undefined ? undefined : templateSrv.replace(val, scopedVars);
    const replaceList = (val: string[] | undefined) =>
      val === undefined ? undefined : val.map((v) => templateSrv.replace(v, scopedVars)).filter((v) => v !== '');

    return {
      ...q,
      siteId: replaceList(q.siteId),
      siteName: replaceList(q.siteName),
      networkDeviceId: replaceString(q.networkDeviceId),
      macAddress: replaceString(q.macAddress),
      parentSiteName: replaceString(q.parentSiteName),
    };
  }

  /**
   * Implements template variable support for the plugin.
   * Supports the following variable types:
   * - priorities: Returns static list P1..P4
   * - issueStatuses: Returns static list ACTIVE/IGNORED/RESOLVED
   * - sites, devices, macs: Fetches recent issues and extracts unique values for the given keys
   */
  async metricFindQuery(raw?: CatalystVariableQuery | string): Promise<MetricFindValue[]> {
    const q = (raw || { type: 'priorities' }) as CatalystVariableQuery;

    switch (q.type) {
      case 'priorities':
        return ['P1', 'P2', 'P3', 'P4'].map((v) => ({ text: v, value: v }));

      case 'issueStatuses':
        return ['ACTIVE', 'IGNORED', 'RESOLVED'].map((v) => ({ text: v, value: v }));

      case 'sites':
        return this.uniqueFromIssues(['siteId'], q.search);

      case 'devices':
        return this.uniqueFromIssues(['deviceId', 'deviceIp', 'device'], q.search);

      case 'macs':
        return this.uniqueFromIssues(['macAddress', 'clientMac'], q.search);

      default:
        return [];
    }
  }

  /**
   * Helper function to fetch recent issues via the backend resource handler
   * and extract unique values for a given set of keys. Used for dynamic template variables.
   * - Queries for issues in the last 24 hours
   * - Paginates up to MAX_PAGES to avoid excessive API calls
   * - Applies optional search filter to returned values
   */
  private async uniqueFromIssues(keys: string[], search?: string): Promise<MetricFindValue[]> {
    const out = new Set<string>();
    const now = Date.now();
    const start = now - 24 * 60 * 60 * 1000;
    const s = (search ?? '').toLowerCase().trim();

    let offset = 1;
    for (let page = 0; page < MAX_PAGES; page++) {
      const params = new URLSearchParams({
        startTime: String(start),
        endTime: String(now),
        limit: String(PAGE_SIZE),
        offset: String(offset),
      });

      try {
        // getResource calls the backend's CallResource handler, which proxies to the Catalyst Center API.
        const data: any = await this.getResource<any>(`issues?${params.toString()}`);
        const arr: any[] = Array.isArray(data) ? data : data?.response ?? [];
        if (!arr.length) {
          break;
        }

        this.log.debug('Fetched issues for variable', { page, count: arr.length });

        for (const it of arr) {
          for (const k of keys) {
            const v = it?.[k];
            if (typeof v === 'string' && v.trim()) {
              const val = v.trim();
              if (!s || val.toLowerCase().includes(s)) {
                out.add(val);
              }
              break;
            }
          }
        }

        if (arr.length < PAGE_SIZE) {
          break;
        }
        offset += PAGE_SIZE;
      } catch (error) {
        // Parse and log error, but continue to return any data we've collected so far
        const catalystError = parseError(error);
        this.log.error('Error fetching issues for variable', catalystError, { page, collectedSoFar: out.size });
        // If this is the first page and we have no data, propagate the error
        if (page === 0 && out.size === 0) {
          throw catalystError;
        }
        // Otherwise, return what we have so far
        break;
      }
    }

    return Array.from(out)
      .sort()
      .map((v) => ({ text: v, value: v }));
  }
}

export default DataSource;
