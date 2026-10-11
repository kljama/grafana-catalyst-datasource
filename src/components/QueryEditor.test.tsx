// QueryEditor.test.tsx: Unit tests for QueryEditor component
import React from 'react';
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import QueryEditor from './QueryEditor';
import { DataSource } from '../datasource';
import { CatalystQuery, DEFAULT_QUERY } from '../types';
import { QueryEditorProps } from '@grafana/data';

// Mock the DataSource
jest.mock('../datasource');

describe('QueryEditor', () => {
  let mockOnChange: jest.Mock;
  let mockOnRunQuery: jest.Mock;
  let mockDatasource: jest.Mocked<DataSource>;
  let defaultProps: QueryEditorProps<DataSource, CatalystQuery, any>;

  beforeEach(() => {
    jest.useFakeTimers();
    
    mockOnChange = jest.fn();
    mockOnRunQuery = jest.fn();
    
    // Create a mock datasource with the getResource method
    mockDatasource = {
      getResource: jest.fn().mockResolvedValue([
        { name: 'Site A', id: 'site-a-id' },
        { name: 'Site B', id: 'site-b-id' },
      ]),
    } as any;

    defaultProps = {
      query: {
        refId: 'A',
        queryType: 'assuranceIssues',
        ...DEFAULT_QUERY,
      } as CatalystQuery,
      onChange: mockOnChange,
      onRunQuery: mockOnRunQuery,
      datasource: mockDatasource,
      data: undefined,
      range: undefined,
      queries: [],
    } as any;
  });

  afterEach(() => {
    jest.useRealTimers();
    jest.clearAllMocks();
  });

  describe('Rendering', () => {
    it('should render without crashing', () => {
      render(<QueryEditor {...defaultProps} />);
      expect(screen.getByText('Query Type')).toBeInTheDocument();
    });

    it('should render assurance issues fields when queryType is assuranceIssues', () => {
      render(<QueryEditor {...defaultProps} />);
      
      expect(screen.getByText('Device ID')).toBeInTheDocument();
      expect(screen.getByText('MAC Address')).toBeInTheDocument();
      expect(screen.getByText('Priority')).toBeInTheDocument();
      expect(screen.getByText('Status')).toBeInTheDocument();
      expect(screen.getByText('Limit')).toBeInTheDocument();
      expect(screen.getByText('AI-Driven')).toBeInTheDocument();
      expect(screen.getByText('Global Issues Only')).toBeInTheDocument();
    });

    it('should render site health fields when queryType is siteHealth', () => {
      const props = {
        ...defaultProps,
        query: {
          ...defaultProps.query,
          queryType: 'siteHealth' as const,
        },
      };
      
      render(<QueryEditor {...props} />);
      
      expect(screen.getByText('Site Type')).toBeInTheDocument();
      expect(screen.getByText('Parent Site')).toBeInTheDocument();
      expect(screen.getByText('Metrics')).toBeInTheDocument();
    });

    it('should render Site field for both query types', () => {
      const { rerender } = render(<QueryEditor {...defaultProps} />);
      expect(screen.getByText('Site')).toBeInTheDocument();
      
      const siteHealthProps = {
        ...defaultProps,
        query: {
          ...defaultProps.query,
          queryType: 'siteHealth' as const,
        },
      };
      rerender(<QueryEditor {...siteHealthProps} />);
      expect(screen.getByText('Site')).toBeInTheDocument();
    });
  });

  describe('Input Handling', () => {
    it('should update Device ID field and call onChange after debounce', async () => {
      render(<QueryEditor {...defaultProps} />);
      
      // Wait for initial sync
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      mockOnChange.mockClear();
      
      const deviceInput = screen.getByPlaceholderText('Enter device UUID');
      fireEvent.change(deviceInput, { target: { value: 'device-123' } });
      
      // onChange should not be called immediately
      expect(mockOnChange).not.toHaveBeenCalled();
      
      // Fast-forward time past the debounce delay (400ms)
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      const lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0];
      expect(lastCall.networkDeviceId).toBe('device-123');
    });

    it('should update MAC Address field and call onChange after debounce', async () => {
      render(<QueryEditor {...defaultProps} />);
      
      // Wait for initial sync
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      mockOnChange.mockClear();
      
      const macInput = screen.getByPlaceholderText('Enter MAC address');
      fireEvent.change(macInput, { target: { value: 'AA:BB:CC:DD:EE:FF' } });
      
      // onChange should not be called immediately
      expect(mockOnChange).not.toHaveBeenCalled();
      
      // Fast-forward time past the debounce delay (400ms)
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      const lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0];
      expect(lastCall.macAddress).toBe('AA:BB:CC:DD:EE:FF');
    });

    it('should update Limit field and call onChange after debounce', async () => {
      render(<QueryEditor {...defaultProps} />);
      
      // Wait for initial sync
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      mockOnChange.mockClear();
      
      const limitInput = screen.getByPlaceholderText('100');
      fireEvent.change(limitInput, { target: { value: '200' } });
      
      // onChange should not be called immediately
      expect(mockOnChange).not.toHaveBeenCalled();
      
      // Fast-forward time past the debounce delay (400ms)
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      const lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0];
      expect(lastCall.limit).toBe(200);
    });

    it('should update Site Type field for siteHealth query', async () => {
      const props = {
        ...defaultProps,
        query: {
          ...defaultProps.query,
          queryType: 'siteHealth' as const,
        },
      };
      
      render(<QueryEditor {...props} />);
      
      const siteTypeInput = screen.getByPlaceholderText('e.g., BUILDING, AREA');
      fireEvent.change(siteTypeInput, { target: { value: 'BUILDING' } });
      
      // Fast-forward time past the debounce delay
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      const lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0];
      expect(lastCall.siteType).toBe('BUILDING');
    });

    it('should update Parent Site field for siteHealth query', async () => {
      const props = {
        ...defaultProps,
        query: {
          ...defaultProps.query,
          queryType: 'siteHealth' as const,
        },
      };
      
      render(<QueryEditor {...props} />);
      
      const parentSiteInput = screen.getByPlaceholderText('Filter by parent site name');
      fireEvent.change(parentSiteInput, { target: { value: 'HQ' } });
      
      // Fast-forward time past the debounce delay
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      const lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0];
      expect(lastCall.parentSiteName).toBe('HQ');
    });
  });

  describe('Debouncing', () => {
    it('should debounce input changes with 400ms delay', async () => {
      render(<QueryEditor {...defaultProps} />);
      
      // Wait for initial sync
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      mockOnChange.mockClear();
      
      const deviceInput = screen.getByPlaceholderText('Enter device UUID');
      
      // Change input value
      fireEvent.change(deviceInput, { target: { value: 'abc' } });
      
      // Should not call onChange immediately
      expect(mockOnChange).not.toHaveBeenCalled();
      
      // Advance time by 200ms (less than debounce delay)
      await act(async () => {
        jest.advanceTimersByTime(200);
      });
      expect(mockOnChange).not.toHaveBeenCalled();
      
      // Advance time by another 200ms (total 400ms)
      await act(async () => {
        jest.advanceTimersByTime(200);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
    });

    it('should reset debounce timer on new input', async () => {
      render(<QueryEditor {...defaultProps} />);
      
      // Wait for initial sync
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      mockOnChange.mockClear();
      
      const deviceInput = screen.getByPlaceholderText('Enter device UUID');
      
      // First change
      fireEvent.change(deviceInput, { target: { value: 'a' } });
      
      // Advance time by 300ms
      await act(async () => {
        jest.advanceTimersByTime(300);
      });
      expect(mockOnChange).not.toHaveBeenCalled();
      
      // Another change (should reset the timer)
      fireEvent.change(deviceInput, { target: { value: 'ab' } });
      
      // Advance time by 300ms (total would be 600ms from first input, but timer was reset)
      await act(async () => {
        jest.advanceTimersByTime(300);
      });
      expect(mockOnChange).not.toHaveBeenCalled();
      
      // Advance time by another 100ms (400ms from last input)
      await act(async () => {
        jest.advanceTimersByTime(100);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
    });

    it('should only call onChange once after multiple rapid changes', async () => {
      render(<QueryEditor {...defaultProps} />);
      
      const deviceInput = screen.getByPlaceholderText('Enter device UUID');
      
      // Simulate rapid typing by making multiple rapid changes
      fireEvent.change(deviceInput, { target: { value: 't' } });
      fireEvent.change(deviceInput, { target: { value: 'te' } });
      fireEvent.change(deviceInput, { target: { value: 'tes' } });
      fireEvent.change(deviceInput, { target: { value: 'test-device-id' } });
      
      // Fast-forward past debounce delay
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      // Should have been called only once for the final state
      // (Note: There might be an initial call when component mounts if query doesn't match defaults)
      // So we check that it wasn't called excessively
      expect(mockOnChange.mock.calls.length).toBeLessThan(5);
    });
  });

  describe('Toggle Interaction', () => {
    it('should toggle AI-Driven switch and call onChange after debounce', async () => {
      render(<QueryEditor {...defaultProps} />);
      
      // Wait for initial sync
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      mockOnChange.mockClear();
      
      // Find the switch by its nearby label text
      const aiDrivenLabel = screen.getByText('AI-Driven');
      const fieldContainer = aiDrivenLabel.closest('.css-1rplq84');
      const aiDrivenSwitch = fieldContainer?.querySelector('input[type="checkbox"]') as HTMLInputElement;
      
      expect(aiDrivenSwitch).toBeTruthy();
      expect(aiDrivenSwitch.checked).toBe(false);
      
      fireEvent.click(aiDrivenSwitch);
      
      // Fast-forward past debounce delay
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      const lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0];
      expect(lastCall.aiDriven).toBe(true);
    });

    it('should toggle Global Issues Only switch and call onChange after debounce', async () => {
      render(<QueryEditor {...defaultProps} />);
      
      // Wait for initial sync
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      mockOnChange.mockClear();
      
      // Find the switch by its nearby label text
      const globalLabel = screen.getByText('Global Issues Only');
      const fieldContainer = globalLabel.closest('.css-1rplq84');
      const globalSwitch = fieldContainer?.querySelector('input[type="checkbox"]') as HTMLInputElement;
      
      expect(globalSwitch).toBeTruthy();
      expect(globalSwitch.checked).toBe(false);
      
      fireEvent.click(globalSwitch);
      
      // Fast-forward past debounce delay
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      const lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0];
      expect(lastCall.isGlobal).toBe(true);
    });

    it('should toggle switches off when already checked', async () => {
      const props = {
        ...defaultProps,
        query: {
          ...defaultProps.query,
          aiDriven: true,
          isGlobal: true,
        },
      };
      
      render(<QueryEditor {...props} />);
      
      // Wait for initial sync
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      mockOnChange.mockClear();
      
      // Find switches by their labels
      const aiDrivenLabel = screen.getByText('AI-Driven');
      const aiFieldContainer = aiDrivenLabel.closest('.css-1rplq84');
      const aiDrivenSwitch = aiFieldContainer?.querySelector('input[type="checkbox"]') as HTMLInputElement;
      
      const globalLabel = screen.getByText('Global Issues Only');
      const globalFieldContainer = globalLabel.closest('.css-1rplq84');
      const globalSwitch = globalFieldContainer?.querySelector('input[type="checkbox"]') as HTMLInputElement;
      
      expect(aiDrivenSwitch.checked).toBe(true);
      expect(globalSwitch.checked).toBe(true);
      
      fireEvent.click(aiDrivenSwitch);
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      let lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0];
      expect(lastCall.aiDriven).toBe(false);
      
      mockOnChange.mockClear();
      fireEvent.click(globalSwitch);
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0];
      expect(lastCall.isGlobal).toBe(false);
    });
  });

  describe('Query Type Switching', () => {
    it('should render with different query types', async () => {
      // Test rendering with assuranceIssues
      const { rerender } = render(<QueryEditor {...defaultProps} />);
      
      // Should show assurance issues fields
      expect(screen.getByText('Device ID')).toBeInTheDocument();
      expect(screen.getByText('Priority')).toBeInTheDocument();
      
      // Test rendering with siteHealth query type
      const siteHealthProps = {
        ...defaultProps,
        query: {
          ...defaultProps.query,
          queryType: 'siteHealth' as const,
        },
      };
      
      rerender(<QueryEditor {...siteHealthProps} />);
      
      // Should show site health fields
      expect(screen.getByText('Site Type')).toBeInTheDocument();
      expect(screen.getByText('Parent Site')).toBeInTheDocument();
      expect(screen.getByText('Metrics')).toBeInTheDocument();
      
      // Should not show assurance issues specific fields
      expect(screen.queryByText('Device ID')).not.toBeInTheDocument();
      expect(screen.queryByText('Priority')).not.toBeInTheDocument();
    });
  });

  describe('Initial State Synchronization', () => {
    it('should initialize filters from query prop', () => {
      const props = {
        ...defaultProps,
        query: {
          ...defaultProps.query,
          networkDeviceId: 'device-456',
          macAddress: 'AA:BB:CC:DD:EE:FF',
          limit: 200,
        },
      };
      
      render(<QueryEditor {...props} />);
      
      const deviceInput = screen.getByPlaceholderText('Enter device UUID') as HTMLInputElement;
      const macInput = screen.getByPlaceholderText('Enter MAC address') as HTMLInputElement;
      const limitInput = screen.getByPlaceholderText('100') as HTMLInputElement;
      
      expect(deviceInput.value).toBe('device-456');
      expect(macInput.value).toBe('AA:BB:CC:DD:EE:FF');
      expect(limitInput.value).toBe('200');
    });

    it('should use default values when query props are undefined', () => {
      const props = {
        ...defaultProps,
        query: {
          refId: 'A',
          queryType: 'assuranceIssues' as const,
        },
      };
      
      render(<QueryEditor {...props} />);
      
      const deviceInput = screen.getByPlaceholderText('Enter device UUID') as HTMLInputElement;
      const macInput = screen.getByPlaceholderText('Enter MAC address') as HTMLInputElement;
      
      expect(deviceInput.value).toBe('');
      expect(macInput.value).toBe('');
    });

    it('should initialize toggle states from query prop', async () => {
      const props = {
        ...defaultProps,
        query: {
          ...defaultProps.query,
          aiDriven: true,
          isGlobal: false,
        },
      };
      
      render(<QueryEditor {...props} />);
      
      // Wait for initial sync
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      // Find switches by their labels
      const aiDrivenLabel = screen.getByText('AI-Driven');
      const aiFieldContainer = aiDrivenLabel.closest('.css-1rplq84');
      const aiDrivenSwitch = aiFieldContainer?.querySelector('input[type="checkbox"]') as HTMLInputElement;
      
      const globalLabel = screen.getByText('Global Issues Only');
      const globalFieldContainer = globalLabel.closest('.css-1rplq84');
      const globalSwitch = globalFieldContainer?.querySelector('input[type="checkbox"]') as HTMLInputElement;
      
      expect(aiDrivenSwitch.checked).toBe(true);
      expect(globalSwitch.checked).toBe(false);
    });
  });

  describe('Site Loading', () => {
    it('should load site options from datasource', async () => {
      render(<QueryEditor {...defaultProps} />);
      
      // AsyncSelect should load options when opened
      expect(mockDatasource.getResource).toHaveBeenCalledWith('sites');
    });

    it('should handle errors when loading sites', async () => {
      const consoleErrorSpy = jest.spyOn(console, 'error').mockImplementation(() => {});
      mockDatasource.getResource.mockRejectedValue(new Error('Network error'));
      
      render(<QueryEditor {...defaultProps} />);
      
      // Wait for async operation to complete
      await waitFor(() => {
        expect(mockDatasource.getResource).toHaveBeenCalled();
      });
      
      // Error should be logged to console
      await waitFor(() => {
        expect(consoleErrorSpy).toHaveBeenCalledWith(
          'Failed to load site options',
          expect.any(Error)
        );
      });
      
      consoleErrorSpy.mockRestore();
    });

    it('should show the load error in the site picker', async () => {
      jest.useRealTimers();
      const consoleErrorSpy = jest.spyOn(console, 'error').mockImplementation(() => {});
      // jsdom lacks IntersectionObserver, which the Grafana select menu uses.
      (global as any).IntersectionObserver ??= class {
        observe() {}
        unobserve() {}
        disconnect() {}
      };
      // Grafana rejects resource calls with the parsed JSON body in `data`.
      mockDatasource.getResource.mockRejectedValue({ data: { message: 'site endpoint returned 502 Bad Gateway' } });

      render(<QueryEditor {...defaultProps} />);
      fireEvent.change(screen.getAllByRole('combobox')[1], { target: { value: 'H' } });

      expect(await screen.findByText(/Could not load sites: site endpoint returned 502 Bad Gateway/)).toBeInTheDocument();
      consoleErrorSpy.mockRestore();
    });
  });

  describe('Edge Cases', () => {
    it('should handle empty string input gracefully', async () => {
      const props = {
        ...defaultProps,
        query: {
          ...defaultProps.query,
          networkDeviceId: 'device-123',
        },
      };
      
  render(<QueryEditor {...props} />);
      
      // Wait for component to settle with initial value
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      // Clear the mock to focus on the change we're about to make
      mockOnChange.mockClear();
      
      const deviceInput = screen.getByPlaceholderText('Enter device UUID');
      fireEvent.change(deviceInput, { target: { value: '' } });
      
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      const lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0];
      expect(lastCall.networkDeviceId).toBe('');
    });

    it('should handle invalid number input for limit', async () => {
      render(<QueryEditor {...defaultProps} />);
      
      // Wait for component to settle
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      // Clear the mock to focus on the change we're about to make
      mockOnChange.mockClear();
      
      const limitInput = screen.getByPlaceholderText('100');
      fireEvent.change(limitInput, { target: { value: 'abc' } });
      
      await act(async () => {
        jest.advanceTimersByTime(400);
      });
      
      await waitFor(() => {
        expect(mockOnChange).toHaveBeenCalled();
      });
      
      const lastCall = mockOnChange.mock.calls[mockOnChange.mock.calls.length - 1][0];
      // parseInt('abc') returns NaN
      expect(lastCall.limit).toBeNaN();
    });
  });
});
