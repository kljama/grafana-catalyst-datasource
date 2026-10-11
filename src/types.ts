import type { DataQuery, DataSourceJsonData } from '@grafana/data';

/**
 * The only query type supported in this version of the plugin.
 * This corresponds to fetching issues/alerts from the Catalyst Center API.
 */
// Allow both legacy 'alerts' name and the API name 'assuranceIssues' for compatibility
export type QueryType = 'alerts' | 'assuranceIssues' | 'siteHealth';

// Define specific, strict types for query parameters to improve type safety.
// Keep the canonical values uppercase as used across tests and UI (P1..P4)
export type CatalystPriority = 'P1' | 'P2' | 'P3' | 'P4';
export type CatalystIssueStatus = 'ACTIVE' | 'RESOLVED' | 'IGNORED';

/**
 * Represents the query structure that is sent from the frontend query editor
 * to the backend.
 */
export interface CatalystQuery extends DataQuery {
  queryType: QueryType;
  endpoint?: 'alerts' | 'siteHealth' | 'issues';
  limit?: number;
  priority?: CatalystPriority[];
  status?: CatalystIssueStatus[];
  networkDeviceId?: string;
  macAddress?: string;
  siteId?: string[];
  issueName?: string;
  // Reserved for future performance-intensive lookups (e.g., full device details)
  enrich?: boolean;
  siteType?: string;
  parentSiteName?: string;
  siteName?: string[];
  parentSiteId?: string;
  metrics?: string[];
  /**
   * true: only AI-driven issues; false or undefined: no filter.
   * 'false' (saved by v1.0.x): only issues that are not AI-driven.
   */
  aiDriven?: boolean | 'false';
  isGlobal?: boolean;
  /** Legacy device filter saved by v1.0.x; see migrateQuery. */
  deviceId?: string;
}

/**
 * Defines the default values for a new query in the query editor.
 */
export const DEFAULT_QUERY: Partial<CatalystQuery> = {
  queryType: 'assuranceIssues',
  limit: 100,
  priority: [],
  status: [],
  networkDeviceId: '',
  macAddress: '',
  siteId: [],
  issueName: '',
  enrich: false,
  siteType: '',
  parentSiteName: '',
  siteName: [],
  parentSiteId: '',
  metrics: [],
  aiDriven: false,
  isGlobal: false,
};

export const metricOptions = [
  { label: 'Client Count', value: 'clientCount' },
  { label: 'Health Score', value: 'healthScore' },
  { label: 'Access Point Count', value: 'accessPointCount' },
  { label: 'Switch Count', value: 'switchCount' },
  { label: 'Router Count', value: 'routerCount' },
];

/**
 * Represents the non-sensitive configuration data for the datasource instance,
 * stored as JSON in the Grafana database.
 */
export interface CatalystJsonData extends DataSourceJsonData {
  /**
   * The base URL of the Catalyst Center API.
   * Example: https://catalyst.example.com
   */
  baseUrl?: string;
  /**
   * The selected API endpoint for queries (e.g., 'alerts', 'siteHealth').
   */
  endpoint?: string;

  /**
   * If true, the backend will not verify the TLS certificate of the API endpoint.
   * This is intended for development or lab environments with self-signed certs.
   */
  insecureSkipVerify?: boolean;
}

/**
 * Represents the sensitive configuration data for the datasource instance.
 * These values are encrypted by Grafana and stored in secureJsonData.
 */
export interface CatalystSecureJsonData {
  username?: string;
  password?: string;
  /**
   * An optional, manually provided authentication token (X-Auth-Token).
   * If this is set, the backend will use it directly and bypass the
   * username/password authentication flow.
   */
  apiToken?: string;
}

/**
 * Defines the structure of queries used in the template variable editor.
 * Each type corresponds to a function that can be called to populate a variable.
 */
export type CatalystVariableQuery =
  | { type: 'priorities' }
  | { type: 'issueStatuses' }
  | { type: 'sites'; search?: string }
  | { type: 'devices'; search?: string }
  | { type: 'macs'; search?: string };
