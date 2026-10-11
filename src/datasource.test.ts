// datasource.test.ts: Unit tests for DataSource class
import { DataSource } from './datasource';
import { DataSourceInstanceSettings, CoreApp } from '@grafana/data';
import { CatalystJsonData, CatalystQuery } from './types';
import { getTemplateSrv } from '@grafana/runtime';

// Mock the template service
jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: jest.fn(),
}));

describe('DataSource', () => {
  let datasource: DataSource;
  let mockInstanceSettings: DataSourceInstanceSettings<CatalystJsonData>;
  let mockTemplateSrv: any;

  beforeEach(() => {
    mockInstanceSettings = {
      id: 1,
      uid: 'test-uid',
      type: 'catalyst-datasource',
      name: 'Test Catalyst',
      meta: {} as any,
      jsonData: {
        baseUrl: 'https://catalyst.example.com',
        endpoint: 'alerts',
      },
      access: 'proxy',
      readOnly: false,
    };

    mockTemplateSrv = {
      replace: jest.fn((value) => value), // Default: no replacement
    };
    (getTemplateSrv as jest.Mock).mockReturnValue(mockTemplateSrv);

    datasource = new DataSource(mockInstanceSettings);
  });

  describe('constructor', () => {
    it('should initialize with instance settings', () => {
      expect(datasource.instanceSettings).toBe(mockInstanceSettings);
    });
  });

  describe('getDefaultQuery', () => {
    it('should return default query with alerts endpoint', () => {
      const query = datasource.getDefaultQuery(CoreApp.Dashboard);
      expect(query.queryType).toBe('alerts');
      expect(query.endpoint).toBe('alerts');
      expect(query.limit).toBe(100);
    });

    it('should use endpoint from config if available', () => {
      mockInstanceSettings.jsonData.endpoint = 'siteHealth';
      datasource = new DataSource(mockInstanceSettings);
      const query = datasource.getDefaultQuery(CoreApp.Dashboard);
      expect(query.queryType).toBe('siteHealth');
      expect(query.endpoint).toBe('siteHealth');
    });

    it('should default to alerts if no endpoint in config', () => {
      mockInstanceSettings.jsonData = {};
      datasource = new DataSource(mockInstanceSettings);
      const query = datasource.getDefaultQuery(CoreApp.Dashboard);
      expect(query.queryType).toBe('alerts');
      expect(query.endpoint).toBe('alerts');
    });
  });

  describe('filterQuery', () => {
    it('should return true for all queries', () => {
      const query: CatalystQuery = {
        refId: 'A',
        queryType: 'alerts',
      };
      expect(datasource.filterQuery(query)).toBe(true);
    });
  });

  describe('applyTemplateVariables', () => {
    it('should replace template variables in query fields', () => {
      mockTemplateSrv.replace = jest.fn((value) => {
        if (value === '$site') {
          return 'site-123';
        }
        if (value === '$device') {
          return 'device-456';
        }
        return value;
      });

      const query: CatalystQuery = {
        refId: 'A',
        queryType: 'alerts',
        siteId: ['$site'],
        networkDeviceId: '$device',
        macAddress: '$mac',
      };

      const result = datasource.applyTemplateVariables(query, {});
      
  expect(mockTemplateSrv.replace).toHaveBeenCalledWith('$site', {});
  expect(mockTemplateSrv.replace).toHaveBeenCalledWith('$device', {});
  expect(result.siteId).toEqual(['site-123']);
  expect(result.networkDeviceId).toBe('device-456');
    });

    it('should handle undefined values', () => {
      const query: CatalystQuery = {
        refId: 'A',
        queryType: 'alerts',
      };

      const result = datasource.applyTemplateVariables(query, {});
      
      expect(result.siteId).toBeUndefined();
      expect(result.networkDeviceId).toBeUndefined();
    });
  });

  describe('metricFindQuery', () => {
    it('should return priorities list', async () => {
      const result = await datasource.metricFindQuery({ type: 'priorities' });
      expect(result).toHaveLength(4);
      expect(result).toEqual([
        { text: 'P1', value: 'P1' },
        { text: 'P2', value: 'P2' },
        { text: 'P3', value: 'P3' },
        { text: 'P4', value: 'P4' },
      ]);
    });

    it('should return issue statuses list', async () => {
      const result = await datasource.metricFindQuery({ type: 'issueStatuses' });
      expect(result).toHaveLength(3);
      expect(result).toEqual([
        { text: 'ACTIVE', value: 'ACTIVE' },
        { text: 'IGNORED', value: 'IGNORED' },
        { text: 'RESOLVED', value: 'RESOLVED' },
      ]);
    });

    it('should default to priorities for undefined query', async () => {
      const result = await datasource.metricFindQuery(undefined);
      expect(result).toHaveLength(4);
      expect(result[0].value).toBe('P1');
    });

    it('should handle unknown query type', async () => {
      const result = await datasource.metricFindQuery({ type: 'unknown' } as any);
      expect(result).toEqual([]);
    });
  });

  describe('uniqueFromIssues', () => {
    beforeEach(() => {
      // Mock getResource method
      datasource.getResource = jest.fn();
    });

    it('should fetch and extract unique site IDs', async () => {
      const mockResponse = {
        response: [
          { siteId: 'site-1' },
          { siteId: 'site-2' },
          { siteId: 'site-1' }, // duplicate
        ],
      };
      (datasource.getResource as jest.Mock).mockResolvedValue(mockResponse);

      const result = await datasource.metricFindQuery({ type: 'sites' });
      
      expect(result).toHaveLength(2);
      expect(result).toEqual([
        { text: 'site-1', value: 'site-1' },
        { text: 'site-2', value: 'site-2' },
      ]);
    });

    it('should filter results by search string', async () => {
      const mockResponse = {
        response: [
          { siteId: 'building-a' },
          { siteId: 'building-b' },
          { siteId: 'warehouse-1' },
        ],
      };
      (datasource.getResource as jest.Mock).mockResolvedValue(mockResponse);

      const result = await datasource.metricFindQuery({ type: 'sites', search: 'building' });
      
      expect(result).toHaveLength(2);
      expect(result[0].value).toBe('building-a');
      expect(result[1].value).toBe('building-b');
    });

    it('should handle empty response', async () => {
      (datasource.getResource as jest.Mock).mockResolvedValue({ response: [] });

      const result = await datasource.metricFindQuery({ type: 'sites' });
      
      expect(result).toEqual([]);
    });

    it('should handle array response format', async () => {
      const mockResponse = [{ siteId: 'site-1' }, { siteId: 'site-2' }];
      (datasource.getResource as jest.Mock).mockResolvedValue(mockResponse);

      const result = await datasource.metricFindQuery({ type: 'sites' });
      
      expect(result).toHaveLength(2);
    });

    it('should search multiple keys for devices', async () => {
      const mockResponse = {
        response: [
          { deviceId: 'device-1' },
          { deviceIp: '192.168.1.1' },
          { device: 'device-3' },
        ],
      };
      (datasource.getResource as jest.Mock).mockResolvedValue(mockResponse);

      const result = await datasource.metricFindQuery({ type: 'devices' });
      
      expect(result).toHaveLength(3);
      expect(result.map((r) => r.value)).toContain('device-1');
      expect(result.map((r) => r.value)).toContain('192.168.1.1');
      expect(result.map((r) => r.value)).toContain('device-3');
    });

    it('should sort results alphabetically', async () => {
      const mockResponse = {
        response: [{ siteId: 'zebra' }, { siteId: 'alpha' }, { siteId: 'beta' }],
      };
      (datasource.getResource as jest.Mock).mockResolvedValue(mockResponse);

      const result = await datasource.metricFindQuery({ type: 'sites' });
      
      expect(result[0].value).toBe('alpha');
      expect(result[1].value).toBe('beta');
      expect(result[2].value).toBe('zebra');
    });

    it('should handle errors and return data collected so far', async () => {
      let callCount = 0;
      (datasource.getResource as jest.Mock).mockImplementation(() => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve({ response: [{ siteId: 'site-1' }] });
        }
        return Promise.reject(new Error('Network error'));
      });

      const result = await datasource.metricFindQuery({ type: 'sites' });
      
      // Should return data from first page even though second page failed
      expect(result).toHaveLength(1);
      expect(result[0].value).toBe('site-1');
    });

    it('should page within the API limits (limit <= 25, offset from 1)', async () => {
      const fullPage = Array.from({ length: 25 }, (_, i) => ({ siteId: `site-${i}` }));
      (datasource.getResource as jest.Mock)
        .mockResolvedValueOnce({ response: fullPage })
        .mockResolvedValueOnce({ response: [{ siteId: 'site-last' }] });

      const result = await datasource.metricFindQuery({ type: 'sites' });

      const calls = (datasource.getResource as jest.Mock).mock.calls.map(([path]) => new URLSearchParams(path.split('?')[1]));
      expect(calls).toHaveLength(2);
      expect(calls.map((p) => [p.get('limit'), p.get('offset')])).toEqual([
        ['25', '1'],
        ['25', '26'],
      ]);
      expect(result).toHaveLength(26);
    });

    it('should throw error if first page fails', async () => {
      (datasource.getResource as jest.Mock).mockRejectedValue(new Error('Network error'));

      await expect(datasource.metricFindQuery({ type: 'sites' })).rejects.toThrow();
    });
  });
});
