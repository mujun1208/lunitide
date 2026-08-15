(() => {
  'use strict';

  const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  // Every document receives the same decorative atmosphere without changing
  // its static reading order or requiring JavaScript for core content.
  let sky = document.querySelector('.sky');
  if (!sky) {
    sky = document.createElement('div');
    sky.className = 'sky';
    document.body.prepend(sky);
  }

  let stars = document.querySelector('.stars');
  if (!stars) {
    stars = document.createElement('div');
    stars.className = 'stars';
    sky.after(stars);
  }

  let moon = document.querySelector('.moon');
  if (!moon) {
    moon = document.createElement('div');
    moon.className = 'moon';
    stars.after(moon);
  }

  [sky, stars, moon].forEach((element) => element.setAttribute('aria-hidden', 'true'));

  if (!reduceMotion && stars.childElementCount === 0) {
    const fragment = document.createDocumentFragment();
    for (let index = 0; index < 72; index += 1) {
      const star = document.createElement('i');
      const size = 0.55 + Math.random() * 2.05;
      star.style.left = `${Math.random() * 100}%`;
      star.style.top = `${Math.random() * 100}%`;
      star.style.width = `${size}px`;
      star.style.height = `${size}px`;
      star.style.animationDelay = `${Math.random() * 4}s`;
      star.style.animationDuration = `${3 + Math.random() * 4}s`;
      fragment.appendChild(star);
    }
    stars.appendChild(fragment);
  }

  // Mark the current document in every static navigation.
  const current = decodeURIComponent(location.pathname.split('/').pop() || 'index.html');
  document.querySelectorAll('nav a[href]').forEach((link) => {
    const target = decodeURIComponent((link.getAttribute('href') || '').split('#')[0]);
    if (target === current) link.setAttribute('aria-current', 'page');
  });

  // The high-fidelity UI blueprint is still useful without JS. This small
  // enhancement only switches the five right-panel visual samples.
  document.querySelectorAll('[data-ui-tab]').forEach((tab) => {
    tab.addEventListener('click', () => {
      const target = tab.getAttribute('data-ui-tab');
      const container = tab.closest('.work-panel');
      if (!container || !target) return;
      container.querySelectorAll('[data-ui-tab]').forEach((item) => {
        item.classList.toggle('active', item === tab);
        item.setAttribute('aria-selected', item === tab ? 'true' : 'false');
      });
      container.querySelectorAll('[data-ui-panel]').forEach((panel) => {
        panel.classList.toggle('active', panel.getAttribute('data-ui-panel') === target);
      });
    });
  });
})();
