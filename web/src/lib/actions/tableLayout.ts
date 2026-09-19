import { prefs } from '../state/prefs.svelte';

export interface TableLayoutOptions {
  /** Stable preference key, shared with no other table. */
  id: string;
  /** Stable column ids in the order the component renders them. */
  columns: readonly string[];
}

const MIN_WIDTH = 56;
const MAX_WIDTH = 640;

/**
 * Add the same resize and reorder behaviour to the few hand-written tables
 * that are too small or specialised to be DataGrids. The action owns only a
 * colgroup and the two controls it adds to each heading; the component keeps
 * owning every row and cell.
 */
export function tableLayout(node: HTMLTableElement, initial: TableLayoutOptions) {
  let options = initial;
  let applying = false;
  let dragged = '';
  const widths: Record<string, number> = {};
  const defaults: Record<string, number> = {};
  const disposers: Array<() => void> = [];

  node.dataset.managedTable = 'true';
  const frame = node.parentElement;
  frame?.classList.add('managed-table-frame');

  let colgroup = node.querySelector<HTMLTableColElement>('colgroup[data-table-layout]');
  if (!colgroup) {
    colgroup = document.createElement('colgroup');
    colgroup.dataset.tableLayout = 'true';
    node.insertBefore(colgroup, node.firstChild);
  }

  function cells(row: HTMLTableRowElement): HTMLElement[] {
    return Array.from(row.children).filter(
      (child): child is HTMLElement => child instanceof HTMLTableCellElement,
    );
  }

  function tagSourceCells(): void {
    for (const row of node.querySelectorAll<HTMLTableRowElement>('tr')) {
      const rowCells = cells(row);
      for (let index = 0; index < rowCells.length; index += 1) {
        if (!rowCells[index]!.dataset.tableColumn && options.columns[index]) {
          rowCells[index]!.dataset.tableColumn = options.columns[index]!;
        }
      }
    }
  }

  function measure(columnID: string): number {
    const candidates = Array.from(
      node.querySelectorAll<HTMLElement>(`[data-table-column="${CSS.escape(columnID)}"]`),
    ).slice(0, 21);
    const content = candidates.reduce((largest, cell) => Math.max(largest, cell.scrollWidth), 0);
    return Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, content + 20));
  }

  function setTableWidth(): void {
    if (matchMedia('(max-width: 768px)').matches) {
      node.style.removeProperty('width');
      return;
    }
    const total = prefs
      .columnOrder(options.id, options.columns)
      .reduce((sum, id) => sum + (widths[id] ?? defaults[id] ?? MIN_WIDTH), 0);
    const custom = options.columns.some((id) => widths[id] !== undefined);
    node.style.width =
      !custom && frame && total >= frame.clientWidth ? '100%' : `${Math.ceil(total)}px`;
  }

  function apply(): void {
    if (applying) return;
    applying = true;
    tagSourceCells();
    const order = prefs.columnOrder(options.id, options.columns);
    for (const id of order) {
      const saved = prefs.columnWidth(options.id, id);
      if (saved !== undefined) widths[id] = saved;
      if (defaults[id] === undefined) defaults[id] = measure(id);
    }
    for (const row of node.querySelectorAll<HTMLTableRowElement>('tr')) {
      const byID = new Map(cells(row).map((cell) => [cell.dataset.tableColumn ?? '', cell]));
      const desired = order.flatMap((id) => {
        const cell = byID.get(id);
        return cell ? [cell] : [];
      });
      const present = cells(row);
      if (desired.some((cell, index) => present[index] !== cell)) {
        for (const cell of desired) row.appendChild(cell);
      }
    }
    const existing = new Map(
      Array.from(colgroup!.children).map((col) => [(col as HTMLElement).dataset.tableColumn, col]),
    );
    const desiredCols: HTMLTableColElement[] = [];
    for (const id of order) {
      let col = existing.get(id) as HTMLTableColElement | undefined;
      if (!col) {
        col = document.createElement('col');
        col.dataset.tableColumn = id;
      }
      col.style.width = `${widths[id] ?? defaults[id] ?? MIN_WIDTH}px`;
      desiredCols.push(col);
    }
    if (desiredCols.some((col, index) => colgroup!.children[index] !== col)) {
      for (const col of desiredCols) colgroup!.appendChild(col);
    }
    setTableWidth();
    applying = false;
  }

  function move(source: string, target: string): void {
    if (!source || source === target) return;
    const order = prefs.columnOrder(options.id, options.columns);
    const from = order.indexOf(source);
    const to = order.indexOf(target);
    if (from < 0 || to < 0) return;
    order.splice(from, 1);
    order.splice(to, 0, source);
    prefs.setColumnOrder(options.id, order);
    apply();
  }

  function addControls(): void {
    for (const heading of node.querySelectorAll<HTMLTableCellElement>('thead th')) {
      const id = heading.dataset.tableColumn;
      if (!id || heading.querySelector('[data-table-layout-control]')) continue;
      heading.classList.add('managed-table-heading');
      const label = heading.textContent?.trim() || 'this';

      const grip = document.createElement('button');
      grip.type = 'button';
      grip.className = 'managed-table-move';
      grip.dataset.tableLayoutControl = 'true';
      grip.draggable = true;
      grip.textContent = '⋮⋮';
      grip.title = 'Drag to reposition; use left and right arrow keys for keyboard control';
      grip.setAttribute('aria-label', `Reposition ${label} column`);
      const onGripKey = (event: KeyboardEvent): void => {
        if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
        event.preventDefault();
        const order = prefs.columnOrder(options.id, options.columns);
        const index = order.indexOf(id);
        const target = order[index + (event.key === 'ArrowLeft' ? -1 : 1)];
        if (target) move(id, target);
      };
      const onDragStart = (event: DragEvent): void => {
        dragged = id;
        event.dataTransfer?.setData('text/plain', id);
      };
      grip.addEventListener('keydown', onGripKey);
      grip.addEventListener('dragstart', onDragStart);
      disposers.push(() => grip.removeEventListener('keydown', onGripKey));
      disposers.push(() => grip.removeEventListener('dragstart', onDragStart));
      heading.insertBefore(grip, heading.firstChild);

      const resize = document.createElement('button');
      resize.type = 'button';
      resize.className = 'managed-table-resizer';
      resize.dataset.tableLayoutControl = 'true';
      resize.title = 'Drag to resize; double-click to restore the default width';
      resize.setAttribute('aria-label', `Resize ${label} column`);
      const onPointerDown = (event: PointerEvent): void => {
        if (event.button !== 0) return;
        event.preventDefault();
        const startX = event.clientX;
        const startWidth = heading.getBoundingClientRect().width;
        const onMove = (moveEvent: PointerEvent): void => {
          widths[id] = Math.max(
            MIN_WIDTH,
            Math.min(MAX_WIDTH, startWidth + moveEvent.clientX - startX),
          );
          apply();
        };
        const onUp = (): void => {
          document.removeEventListener('pointermove', onMove);
          prefs.setColumnWidth(options.id, id, widths[id]!);
        };
        document.addEventListener('pointermove', onMove);
        document.addEventListener('pointerup', onUp, { once: true });
      };
      const onResizeKey = (event: KeyboardEvent): void => {
        if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
        event.preventDefault();
        const current = heading.getBoundingClientRect().width;
        widths[id] = Math.max(
          MIN_WIDTH,
          Math.min(MAX_WIDTH, current + (event.key === 'ArrowRight' ? 8 : -8)),
        );
        prefs.setColumnWidth(options.id, id, widths[id]!);
        apply();
      };
      const onDoubleClick = (): void => {
        delete widths[id];
        prefs.clearColumnWidth(options.id, id);
        apply();
      };
      const onDragOver = (event: DragEvent): void => {
        if (dragged) event.preventDefault();
      };
      const onDrop = (event: DragEvent): void => {
        event.preventDefault();
        move(dragged, id);
        dragged = '';
      };
      resize.addEventListener('pointerdown', onPointerDown);
      resize.addEventListener('keydown', onResizeKey);
      resize.addEventListener('dblclick', onDoubleClick);
      heading.addEventListener('dragover', onDragOver);
      heading.addEventListener('drop', onDrop);
      disposers.push(() => resize.removeEventListener('pointerdown', onPointerDown));
      disposers.push(() => resize.removeEventListener('keydown', onResizeKey));
      disposers.push(() => resize.removeEventListener('dblclick', onDoubleClick));
      disposers.push(() => heading.removeEventListener('dragover', onDragOver));
      disposers.push(() => heading.removeEventListener('drop', onDrop));
      heading.appendChild(resize);
    }
  }

  const onViewport = (): void => setTableWidth();
  tagSourceCells();
  addControls();
  apply();
  const observer = new MutationObserver(() => {
    if (applying) return;
    queueMicrotask(() => {
      tagSourceCells();
      addControls();
      apply();
    });
  });
  observer.observe(node.tBodies.item(0) ?? node, { childList: true, subtree: true });
  addEventListener('resize', onViewport);

  return {
    update(next: TableLayoutOptions) {
      options = next;
      tagSourceCells();
      addControls();
      apply();
    },
    destroy() {
      observer.disconnect();
      removeEventListener('resize', onViewport);
      for (const dispose of disposers) dispose();
      frame?.classList.remove('managed-table-frame');
    },
  };
}
