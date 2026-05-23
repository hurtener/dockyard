<!--
  Table.svelte — the create_table widget renderer.

  Composes web/ui's DataTable. Columns + rows arrive shaped from the typed
  contract; this file is responsible for nothing more than wiring them into
  DataTable's props. The large fixture exercises DataTable's pagination
  internals; the empty fixture is handled upstream by PageState.
-->
<script lang="ts">
  import { DataTable } from '@dockyard/ui';
  import type { Column, Row } from '@dockyard/ui';

  type TablePayload = {
    kind: 'table';
    columns: Array<{ key: string; label: string; type: string; sortable?: boolean }>;
    rows: Array<Record<string, unknown>>;
    sort?: { column: string; dir: string } | null;
  };

  interface Props {
    payload: TablePayload;
  }
  let { payload }: Props = $props();

  // Map the contract column shape onto the web/ui Column shape.
  let columns: Column[] = $derived(
    payload.columns.map((c) => ({
      key: c.key,
      label: c.label,
      sortable: !!c.sortable,
    })),
  );
  let rows: Row[] = $derived(payload.rows as Row[]);
</script>

<section class="table" data-testid="widget-table">
  <DataTable {columns} {rows} pageSize={50} />
</section>

<style>
  .table {
    display: block;
  }
</style>
