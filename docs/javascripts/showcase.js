/* The documents and native accordions remain readable without this script. */
(() => {
  const drawer = document.getElementById('__drawer');
  const menu = document.querySelector('[data-docs-menu]');
  const close = document.querySelector('[data-docs-close]');
  const navigation = document.getElementById('zoomies-navigation');
  const searchToggle = document.getElementById('__search');
  const query = document.querySelector('[data-md-component="search-query"]');
  const mobile = matchMedia('(max-width: 76.234375em)');
  let searchOrigin;
  let previousOverflow = '';
  const toggle = (input, value) => {
    if (!input) return;
    input.checked = value;
    input.dispatchEvent(new Event('change', { bubbles: true }));
  };
  const activateLabel = (label) =>
    label?.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        label.click();
      }
    });
  menu?.addEventListener('click', (event) => {
    event.preventDefault();
    toggle(drawer, !drawer.checked);
  });
  close?.addEventListener('click', (event) => {
    event.preventDefault();
    toggle(drawer, false);
    menu?.focus();
  });
  activateLabel(menu);
  activateLabel(close);
  const background = [...document.querySelectorAll('#main, .footer, .header-inner')];
  drawer?.addEventListener('change', () => {
    const open = drawer.checked && mobile.matches;
    menu?.setAttribute('aria-expanded', String(open));
    background.forEach((element) => {
      element.inert = open;
    });
    if (open) {
      previousOverflow = document.body.style.overflow;
      document.body.style.overflow = 'hidden';
      navigation.setAttribute('role', 'dialog');
      navigation.setAttribute('aria-modal', 'true');
      navigation.setAttribute('aria-label', 'Site navigation');
      close?.focus();
    } else {
      document.body.style.overflow = previousOverflow;
      navigation.removeAttribute('role');
      navigation.removeAttribute('aria-modal');
      navigation.removeAttribute('aria-label');
    }
  });
  mobile.addEventListener('change', () => {
    if (!mobile.matches) toggle(drawer, false);
  });
  navigation?.querySelectorAll('a').forEach((link) =>
    link.addEventListener('click', () => {
      if (mobile.matches) toggle(drawer, false);
    }),
  );
  const openSearch = (origin) => {
    searchOrigin = origin;
    if (drawer?.checked) toggle(drawer, false);
    toggle(searchToggle, true);
    query?.focus();
  };
  document
    .querySelectorAll('[data-docs-search]')
    .forEach((button) => button.addEventListener('click', () => openSearch(button)));
  document.querySelectorAll('[data-search-shortcut]').forEach((label) => {
    label.textContent = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘ K' : 'Ctrl K';
  });
  // Material observes keyup; paste and speech entry also need to update results.
  query?.addEventListener('input', () =>
    query.dispatchEvent(new KeyboardEvent('keyup', { bubbles: true })),
  );
  document.addEventListener(
    'keydown',
    (event) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        if (searchToggle?.checked) toggle(searchToggle, false);
        else openSearch(document.activeElement);
      }
      if (event.key === 'Escape' && searchToggle?.checked) {
        event.preventDefault();
        toggle(searchToggle, false);
        searchOrigin?.focus();
      } else if (event.key === 'Escape' && drawer?.checked) {
        toggle(drawer, false);
        menu?.focus();
      }
      if (event.key === 'Tab' && drawer?.checked && mobile.matches) {
        const visible = [...navigation.querySelectorAll('a, summary, [tabindex="0"]')].filter(
          (element) => element.getClientRects().length,
        );
        const first = visible[0],
          last = visible.at(-1);
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }
    },
    true,
  );
  const theme = document.querySelector('[data-theme-toggle]');
  const updateTheme = () => {
    const dark = document.body.dataset.mdColorScheme === 'slate';
    theme?.setAttribute('aria-label', `Switch to ${dark ? 'light' : 'dark'} theme`);
    document.querySelectorAll('[data-image]').forEach((link) => {
      link.href = link.href.replace(/-(?:light|dark)\.webp$/, `-${dark ? 'dark' : 'light'}.webp`);
    });
  };
  updateTheme();
  new MutationObserver(updateTheme).observe(document.body, {
    attributes: true,
    attributeFilter: ['data-md-color-scheme'],
  });
  theme?.addEventListener('click', () => {
    const scheme = document.body.dataset.mdColorScheme === 'slate' ? 'default' : 'slate';
    document.querySelector(`input[data-md-color-scheme="${scheme}"]`)?.click();
  });

  const tabs = [...document.querySelectorAll('[data-tour]')];
  const select = (tab, focus = false) => {
    tabs.forEach((item) => {
      const active = item === tab;
      item.setAttribute('aria-selected', String(active));
      item.tabIndex = active ? 0 : -1;
      document.getElementById(`tour-${item.dataset.tour}`).hidden = !active;
    });
    if (focus) tab.focus();
  };
  tabs.forEach((tab, index) => {
    tab.addEventListener('click', () => select(tab));
    tab.addEventListener('keydown', (event) => {
      const next = {
        ArrowRight: (index + 1) % tabs.length,
        ArrowLeft: (index + tabs.length - 1) % tabs.length,
        Home: 0,
        End: tabs.length - 1,
      }[event.key];
      if (next !== undefined) {
        event.preventDefault();
        select(tabs[next], true);
      }
    });
  });
  if (tabs.length) select(tabs[0]);
  document.querySelectorAll('[data-style]').forEach((button) =>
    button.addEventListener('click', () => {
      document.querySelectorAll('[data-style]').forEach((item) => {
        item.classList.toggle('active', item === button);
        item.setAttribute('aria-pressed', String(item === button));
      });
      const picture = document.querySelector('#hero-screen picture');
      for (const [name, scheme] of [
        ['on-light', 'light'],
        ['on-dark', 'dark'],
      ]) {
        const image = picture.querySelector(`.${name}`);
        const prefix = image.src.replace(
          /(?:mission-control\/)?overview-(?:light|dark)\.webp$/,
          '',
        );
        image.src = `${prefix}${button.dataset.style}-${scheme}.webp`;
      }
    }),
  );
  const filters = [...document.querySelectorAll('[data-filter]')];
  const filterGuides = (category) => {
    let count = 0;
    filters.forEach((button) => {
      const active = button.dataset.filter === category;
      button.classList.toggle('active', active);
      button.setAttribute('aria-pressed', String(active));
    });
    document.querySelectorAll('.guide-all [data-category]').forEach((card) => {
      card.hidden = category !== 'All guides' && card.dataset.category !== category;
      if (!card.hidden) count++;
    });
    document.getElementById('guide-count').textContent =
      `${count} ${count === 1 ? 'GUIDE' : 'GUIDES'}`;
  };
  filters.forEach((button) =>
    button.addEventListener('click', () => {
      filterGuides(button.dataset.filter);
      const address = new URL(location.href);
      if (button.dataset.filter === 'All guides') address.searchParams.delete('topic');
      else address.searchParams.set('topic', button.dataset.filter);
      history.replaceState(null, '', address);
    }),
  );
  if (filters.length) {
    const topic = new URL(location.href).searchParams.get('topic');
    filterGuides(filters.some((button) => button.dataset.filter === topic) ? topic : 'All guides');
  }
  document.querySelectorAll('[data-copy]').forEach((button) =>
    button.addEventListener('click', async () => {
      const status = button.parentElement.querySelector('.copy-status');
      try {
        await navigator.clipboard.writeText(button.dataset.copy);
        status.textContent = 'Copied';
        setTimeout(() => {
          status.textContent = '';
        }, 2000);
      } catch {
        const range = document.createRange();
        range.selectNodeContents(button.parentElement.querySelector('code'));
        const selection = getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
        status.textContent = 'Command selected';
      }
    }),
  );
  const revealHash = () => {
    let target;
    try {
      target = document.getElementById(decodeURIComponent(location.hash.slice(1)));
    } catch {
      return;
    }
    if (!target) return;
    let parent = target.parentElement,
      changed = false;
    while (parent) {
      if (parent.tagName === 'DETAILS' && !parent.open) {
        parent.open = true;
        changed = true;
      }
      parent = parent.parentElement;
    }
    if (changed) requestAnimationFrame(() => target.scrollIntoView());
  };
  revealHash();
  addEventListener('hashchange', revealHash);
})();
