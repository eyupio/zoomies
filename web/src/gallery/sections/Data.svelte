<!--
  The real DataGrid, in each of the four states a fetch can leave it in. The
  fetcher resolves against fixture rows in the browser, so the gallery needs no
  controller and no network: the point is the grid's own chrome, not data.

  On a Mission Control phone a grid with no saved choice falls back to Cards;
  Classic falls back to Rows. Widening and narrowing the window here is the
  quickest way to review both.
-->
<script lang="ts">
  import Button from '$lib/components/Button.svelte';
  import DataGrid, { type GridColumn, type GridPage } from '$lib/components/DataGrid.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import StateCell from '$lib/components/StateCell.svelte';
  import { Server } from '@lucide/svelte';
  import type { RunnerState } from '$lib/api/types';
  import { runnerStatus } from '$lib/status';

  type GalleryRow = {
    id: string;
    name: string;
    pool: string;
    state: RunnerState;
    age: string;
  };

  type FetchMode = 'ready' | 'empty' | 'loading' | 'error';

  const rows: GalleryRow[] = [
    { id: 'run_demo00', name: 'run_demo00', pool: 'linux-x64', state: 'busy', age: '1m 40s' },
    { id: 'run_demo01', name: 'run_demo01', pool: 'linux-x64', state: 'idle', age: '6m' },
    { id: 'run_demo02', name: 'run_demo02', pool: 'windows', state: 'provisioning', age: '20s' },
    { id: 'run_demo03', name: 'run_demo03', pool: 'linux-x64', state: 'draining', age: '11m' },
  ];

  let mode = $state<FetchMode>('ready');

  const modes = [
    { value: 'ready', label: 'Ready', name: 'Rows arrived' },
    { value: 'empty', label: 'Empty', name: 'A filter that matches nothing' },
    { value: 'loading', label: 'Loading', name: 'The first fetch has not answered' },
    { value: 'error', label: 'Error', name: 'The fetch failed' },
  ];

  async function fetchRows(): Promise<GridPage<GalleryRow>> {
    switch (mode) {
      case 'empty':
        return { items: [], total: 0 };
      case 'loading':
        // Never settles: this is the skeleton, and it is meant to stay put.
        return new Promise(() => {});
      case 'error':
        throw new Error('The controller did not answer. Check that the process is running.');
      default:
        return { items: rows, total: rows.length };
    }
  }

  const columns: GridColumn<GalleryRow>[] = [
    { id: 'name', header: 'Runner', sortable: true, value: (row) => row.name },
    { id: 'pool', header: 'Pool', sortable: true, value: (row) => row.pool },
    { id: 'state', header: 'State', cell: stateCell },
    { id: 'age', header: 'Age', align: 'end', value: (row) => row.age },
  ];
</script>

{#snippet stateCell(row: GalleryRow)}
  <StateCell status={runnerStatus(row.state, false)} />
{/snippet}

<!--
  The list card every list page draws: the grid, bare, inside a Panel that
  carries the count over its title and the primary filter as chips.
-->
<Panel
  title="Data grid"
  icon={Server}
  eyebrow="11 known, 9 live"
  description="Ready, empty, loading and failed, with the phone layout the saved choice or the style decides."
  flush
>
  {#snippet actions()}
    <Segmented
      label="Grid state"
      options={modes}
      value={mode}
      onchange={(v) => (mode = v as FetchMode)}
    />
  {/snippet}
  {#snippet toolbar()}
    <div role="group" aria-label="Filter by runner state">
      <Button size="sm" variant="secondary">All</Button>
    </div>
  {/snippet}
  <!--
    Keyed by mode on purpose. The grid deliberately keeps last-known rows on
    screen while a later fetch is out -- a live refresh that flashed back to
    skeletons every second would be unreadable -- so switching its filters is
    not enough to reach the loading state. A fresh mount per mode is the only
    honest way to show all four.
  -->
  {#key mode}
    <DataGrid
      gridId="gallery-demo"
      label="Gallery runners"
      noun="runners"
      bare
      {columns}
      fetcher={fetchRows}
      rowId={(row) => row.id}
      filters={{ mode }}
      emptyTitle="No runners match these filters"
      emptyDescription="Clear a filter to see the rest of the fleet."
    >
      {#snippet emptyAction()}
        <Button onclick={() => (mode = 'ready')}>Clear filters</Button>
      {/snippet}
    </DataGrid>
  {/key}
</Panel>
