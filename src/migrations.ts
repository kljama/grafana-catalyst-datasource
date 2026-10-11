// migrations.ts: brings saved queries of older plugin versions into the current shape.
//
// The v1.0.4/v1.0.5 query editor saved siteId as a string, a single issueStatus,
// deviceId instead of networkDeviceId and aiDriven as the string 'true'/'false'.
// The backend accepts these shapes too (pkg/backend/model.go), but the editor and
// applyTemplateVariables need arrays to work with.
import type { CatalystQuery } from './types';

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Converts a saved list value (array, single string, empty string or null) to a string array. */
export function toStringList(value: unknown): string[] {
  const items = Array.isArray(value) ? value : [value];
  return items
    .filter((v): v is string => typeof v === 'string')
    .map((v) => v.trim())
    .filter((v) => v !== '');
}

/**
 * Returns the query in the current shape. The input is not modified.
 * - siteId, siteName, priority, status, metrics: always arrays when present;
 *   a string becomes [s], and '' or null becomes [].
 * - issueStatus becomes status, severity becomes priority (unless already set).
 * - deviceId holding a device UUID becomes networkDeviceId. Other deviceId values
 *   (an IP address, a name) are kept for the backend, which maps or reports them.
 * - aiDriven 'true' becomes true. 'false' stays the string 'false', which means
 *   "only non-AI issues" and differs from the switch's false ("no filter").
 */
export function migrateQuery(query: CatalystQuery): CatalystQuery {
  const q: Record<string, unknown> = { ...query };

  for (const key of ['siteId', 'siteName', 'priority', 'status', 'metrics']) {
    if (q[key] !== undefined) {
      q[key] = toStringList(q[key]);
    }
  }

  const issueStatus = toStringList(q.issueStatus);
  if (issueStatus.length > 0 && toStringList(q.status).length === 0) {
    q.status = issueStatus.map((s) => s.toUpperCase());
  }
  delete q.issueStatus;

  const severity = toStringList(q.severity);
  if (severity.length > 0 && toStringList(q.priority).length === 0) {
    q.priority = severity.map((s) => s.toUpperCase());
  }
  delete q.severity;

  const deviceId = typeof q.deviceId === 'string' ? q.deviceId.trim() : '';
  if (deviceId && UUID.test(deviceId)) {
    if (!q.networkDeviceId) {
      q.networkDeviceId = deviceId;
    }
    delete q.deviceId;
  }

  if (q.aiDriven === 'true') {
    q.aiDriven = true;
  }

  return q as unknown as CatalystQuery;
}
