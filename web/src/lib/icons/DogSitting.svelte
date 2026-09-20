<!--
  A dog sitting, in profile: the icon for a runner whose pool has elastic CPU
  off and which is therefore held at exactly its share -- not moving, and doing
  as it was told.

  Lucide has a dog's face and a paw, and neither says "staying put". This is
  drawn to Lucide's own rules (a 24-unit box, 2-unit round strokes, nothing
  filled) and rendered through its Icon component, so it takes the same size,
  stroke and class props as every other status icon and sits beside them
  without looking like a visitor.
-->
<script lang="ts">
  import { Icon } from '@lucide/svelte';
  import type { LucideIconNode, LucideProps } from '@lucide/svelte';

  let props: LucideProps = $props();

  const node: LucideIconNode[] = [
    // The head, and the muzzle it points forward.
    ['circle', { cx: '16.5', cy: '6.5', r: '3' }],
    ['path', { d: 'M19.5 7h2' }],
    // One ear, up.
    ['path', { d: 'M14.8 4.2 13.5 2.5' }],
    // The back, down over the haunch to the ground, and along it to the paws.
    ['path', { d: 'M14 8.5C9.5 10 6 13 5.5 17.5c0 2 1 3.5 3 3.5h8' }],
    // Both front legs, straight: a dog that is sitting rather than lying.
    ['path', { d: 'M16.5 9.5V21' }],
    ['path', { d: 'M13 15v6' }],
    // The tail, resting behind.
    ['path', { d: 'M5.5 17.5C4 17.5 2.5 16.5 2.5 14.5' }],
  ];
</script>

<Icon {...props} iconNode={node} />
