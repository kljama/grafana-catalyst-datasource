import React, { useEffect, useRef, useState } from 'react';
import { Field, Input, InlineField, MultiSelect, Switch, AsyncSelect, Select } from '@grafana/ui';
import type { QueryEditorProps, SelectableValue } from '@grafana/data';
import { DataSource } from '../datasource';
import {
  CatalystQuery,
  CatalystJsonData,
  CatalystPriority,
  CatalystIssueStatus,
  DEFAULT_QUERY,
  QueryType,
} from '../types';

// Props: Provided by Grafana plugin system
type Props = QueryEditorProps<DataSource, CatalystQuery, CatalystJsonData>;

// useDebounced: Custom hook to debounce value changes for smoother UX and reduced backend load
function useDebounced<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const h = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(h);
  }, [value, delayMs]);
  return debounced;
}

// Priority dropdown options
const PRIORITY_OPTIONS: Array<SelectableValue<CatalystPriority>> = [
  { label: 'P1', value: 'P1' },
  { label: 'P2', value: 'P2' },
  { label: 'P3', value: 'P3' },
  { label: 'P4', value: 'P4' },
];

// Issue status dropdown options
const STATUS_OPTIONS: Array<SelectableValue<CatalystIssueStatus>> = [
  { label: 'ACTIVE', value: 'ACTIVE' },
  { label: 'RESOLVED', value: 'RESOLVED' },
  { label: 'IGNORED', value: 'IGNORED' },
];

const siteHealthMetricOptions: Array<SelectableValue<string>> = [
  { label: 'Client Count (Total)', value: 'clientCount' },
  { label: 'Total Clients (numberOfClients)', value: 'numberOfClients' },
  { label: 'Connected Wired Clients (numberOfWiredClients)', value: 'numberOfWiredClients' },
  { label: 'Total Connected Wired Clients', value: 'totalNumberOfConnectedWiredClients' },
  { label: 'Active Wireless Clients (numberOfWirelessClients)', value: 'numberOfWirelessClients' },
  { label: 'Total Active Wireless Clients', value: 'totalNumberOfActiveWirelessClients' },
  { label: 'Wired Clients (Good)', value: 'wiredGoodClients' },
  { label: 'Wireless Clients (Good)', value: 'wirelessGoodClients' },
  { label: 'Healthy Network Device %', value: 'healthyNetworkDevicePercentage' },
  { label: 'Healthy Clients %', value: 'healthyClientsPercentage' },
  { label: 'Wired Client Health', value: 'clientHealthWired' },
  { label: 'Wireless Client Health', value: 'clientHealthWireless' },
  { label: 'Number of Network Devices', value: 'numberOfNetworkDevice' },
  { label: 'Network Health Average', value: 'networkHealthAverage' },
  { label: 'Network Health (Access)', value: 'networkHealthAccess' },
  { label: 'Network Health (Core)', value: 'networkHealthCore' },
  { label: 'Network Health (AP)', value: 'networkHealthAP' },
  { label: 'Network Health (WLC)', value: 'networkHealthWLC' },
  { label: 'Network Health (Switch)', value: 'networkHealthSwitch' },
  { label: 'Access Devices (Total)', value: 'accessTotalCount' },
  { label: 'Access Devices (Good)', value: 'accessGoodCount' },
  { label: 'AP Devices (Good)', value: 'apDeviceGoodCount' },
  { label: 'AP Devices (Total)', value: 'apDeviceTotalCount' },
  { label: 'Switch Devices (Good)', value: 'switchDeviceGoodCount' },
  { label: 'Switch Devices (Total)', value: 'switchDeviceTotalCount' },
];

// Define a type for the filter state
type Filters = Omit<Partial<CatalystQuery>, 'refId' | 'queryType' | 'enrich'>;

const queryTypeOptions: Array<SelectableValue<QueryType>> = [
  { label: 'Assurance Issues', value: 'assuranceIssues' },
  { label: 'Site Health', value: 'siteHealth' },
];

const QueryEditor: React.FC<Props> = ({ query, onChange, onRunQuery, datasource }) => {
  const { queryType } = query;

  // Unified state for all filters
  const [filters, setFilters] = useState<Filters>({
    siteId: query.siteId ?? DEFAULT_QUERY.siteId,
    networkDeviceId: query.networkDeviceId ?? DEFAULT_QUERY.networkDeviceId,
    macAddress: query.macAddress ?? DEFAULT_QUERY.macAddress,
    priority: query.priority ?? DEFAULT_QUERY.priority,
    status: query.status ?? DEFAULT_QUERY.status,
    limit: query.limit ?? DEFAULT_QUERY.limit,
    metrics: query.metrics ?? DEFAULT_QUERY.metrics,
    parentSiteName: query.parentSiteName ?? DEFAULT_QUERY.parentSiteName,
    siteName: query.siteName ?? DEFAULT_QUERY.siteName,
    aiDriven: query.aiDriven ?? DEFAULT_QUERY.aiDriven,
    isGlobal: query.isGlobal ?? DEFAULT_QUERY.isGlobal,
  });

  // Debounced version of the filters
  const debouncedFilters = useDebounced(filters, 400);

  // Effect: Synchronize debounced state with parent query object
  // Prevents infinite loops by tracking last signature
  const lastSig = useRef<string>('');
  useEffect(() => {
    const next: CatalystQuery = {
      ...query,
      ...debouncedFilters,
    };
    const sig = JSON.stringify(next);

    if (sig !== lastSig.current) {
      lastSig.current = sig;
      onChange(next);
      // Do NOT auto-run query; only update state.
    }
  }, [debouncedFilters, onChange, query]);

  const loadSiteOptions = async (inputValue: string) => {
    try {
      const sites = await datasource.getResource('sites');
      const siteOptions = sites.map((site: { name: string; id: string }) => ({
        label: site.name,
        value: site.id,
      }));

      if (!inputValue) {
        return siteOptions;
      }

      const filteredSites = siteOptions.filter((option: SelectableValue<string>) =>
        option.label!.toLowerCase().includes(inputValue.toLowerCase())
      );

      return filteredSites;
    } catch (error) {
      console.error('Failed to load site options', error);
      return [];
    }
  };

  // Render common and endpoint-specific filters
  return (
    <div className="gf-form-group">
      <InlineField label="Query Type" labelWidth={14}>
        <Select
          width={40}
          options={queryTypeOptions}
          value={queryType}
          onChange={(v: SelectableValue<QueryType>) => {
            onChange({ ...query, queryType: v.value! });
          }}
        />
      </InlineField>

      {queryType === 'siteHealth' ? (
        <>
          <InlineField label="Site Type" labelWidth={14}>
            <Input
              width={40}
              value={filters.siteType}
              onChange={(e) => setFilters({ ...filters, siteType: e.currentTarget.value })}
              placeholder="e.g., BUILDING, AREA"
            />
          </InlineField>
          <InlineField label="Parent Site" labelWidth={14}>
            <Input
              width={40}
              value={filters.parentSiteName}
              onChange={(e) => setFilters({ ...filters, parentSiteName: e.currentTarget.value })}
              placeholder="Filter by parent site name"
            />
          </InlineField>
          <InlineField label="Site" labelWidth={14}>
            <AsyncSelect
              isMulti
              width={40}
              loadOptions={loadSiteOptions}
              defaultOptions
              value={filters.siteId?.map((id, index) => ({ label: filters.siteName?.[index] || id, value: id }))}
              onChange={(v) => {
                const siteIds = v.map((item: SelectableValue<string>) => item.value!);
                const siteNames = v.map((item: SelectableValue<string>) => item.label!);
                setFilters({ ...filters, siteId: siteIds, siteName: siteNames });
              }}
              isClearable
              placeholder="Select a site"
            />
          </InlineField>
          <Field label="Metrics">
            <MultiSelect
              options={siteHealthMetricOptions}
              value={filters.metrics}
              onChange={(v) => setFilters({ ...filters, metrics: v.map((item) => item.value!) })}
            />
          </Field>
        </>
      ) : (
        <>
          <InlineField label="Site" labelWidth={14}>
            <AsyncSelect
              isMulti
              width={40}
              loadOptions={loadSiteOptions}
              defaultOptions
              value={filters.siteId?.map((id, index) => ({ label: filters.siteName?.[index] || id, value: id }))}
              onChange={(v) => {
                const siteIds = v.map((item: SelectableValue<string>) => item.value!);
                const siteNames = v.map((item: SelectableValue<string>) => item.label!);
                setFilters({ ...filters, siteId: siteIds, siteName: siteNames });
              }}
              isClearable
              placeholder="Select sites to filter"
            />
          </InlineField>
          <InlineField label="Device ID" labelWidth={14}>
            <Input
              width={40}
              value={filters.networkDeviceId}
              onChange={(e) => setFilters({ ...filters, networkDeviceId: e.currentTarget.value })}
              placeholder="Enter device UUID"
            />
          </InlineField>
          <InlineField label="MAC Address" labelWidth={14}>
            <Input
              width={40}
              value={filters.macAddress}
              onChange={(e) => setFilters({ ...filters, macAddress: e.currentTarget.value })}
              placeholder="Enter MAC address"
            />
          </InlineField>
          <Field label="Priority">
            <MultiSelect
              options={PRIORITY_OPTIONS}
              value={filters.priority}
              onChange={(v) => setFilters({ ...filters, priority: v.map((item) => item.value!) })}
            />
          </Field>
          <Field label="Status">
            <MultiSelect
              options={STATUS_OPTIONS}
              value={filters.status}
              onChange={(v) => setFilters({ ...filters, status: v.map((item) => item.value!) })}
            />
          </Field>
          <InlineField label="Limit" labelWidth={14}>
            <Input
              width={20}
              type="number"
              value={filters.limit}
              onChange={(e) => setFilters({ ...filters, limit: parseInt(e.currentTarget.value, 10) })}
              placeholder="100"
            />
          </InlineField>
          <Field label="AI-Driven" description="If enabled, only returns issues identified by the AI engine.">
            <Switch
              value={!!filters.aiDriven}
              onChange={(e) => setFilters({ ...filters, aiDriven: e.currentTarget.checked })}
            />
          </Field>
          <Field label="Global Issues Only" description="If enabled, only returns issues that impact multiple sites or devices.">
            <Switch
              value={!!filters.isGlobal}
              onChange={(e) => setFilters({ ...filters, isGlobal: e.currentTarget.checked })}
            />
          </Field>
        </>
      )}
    </div>
  );
};

export default QueryEditor;
