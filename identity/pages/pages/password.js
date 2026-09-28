// Keep password visibility local to the current page and field.
document.querySelectorAll('input[type="password"]').forEach((input, index) => {
  const label = input.closest('label');
  if (!label) return;
  const fieldName = label.textContent.trim().toLowerCase();
  const wrapper = document.createElement('div');
  wrapper.className = 'password-field';
  label.before(wrapper);
  wrapper.append(label);
  input.id ||= `password-${index}`;
  const toggle = document.createElement('button');
  toggle.type = 'button';
  toggle.className = 'password-toggle';
  toggle.setAttribute('aria-controls', input.id);
  toggle.setAttribute('aria-pressed', 'false');
  toggle.setAttribute('aria-label', `Show ${fieldName}`);
  toggle.title = `Show ${fieldName}`;
  toggle.innerHTML = '<svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/></svg>';
  toggle.addEventListener('click', () => {
    const visible = input.type === 'password';
    input.type = visible ? 'text' : 'password';
    toggle.setAttribute('aria-pressed', String(visible));
    toggle.setAttribute('aria-label', `${visible ? 'Hide' : 'Show'} ${fieldName}`);
    toggle.title = `${visible ? 'Hide' : 'Show'} ${fieldName}`;
  });
  wrapper.append(toggle);
});

// Bring the corner light to life: at random intervals (up to ~3 minutes) play
// one short sequence, or none, so the rhythm never settles into a pattern.
(() => {
  const glow = document.querySelector('.glow');
  if (!glow) return;
  if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;

  // Weighted pool: mostly subtle, an occasional bolder sequence, and a real
  // chance of resting quietly. Higher weight = more likely.
  const pool = [
    { name: null, weight: 3 }, // rest, play nothing this cycle
    { name: 'glow-breathe', weight: 5 },
    { name: 'glow-drift', weight: 4 },
    { name: 'glow-blink', weight: 3 },
    { name: 'glow-flicker', weight: 1 }, // rare, bolder
    { name: 'glow-sweep', weight: 2 }, // rare, bolder
    { name: 'glow-abort', weight: 2 }, // rare, bolder: commit then backtrack
  ];
  const totalWeight = pool.reduce((sum, item) => sum + item.weight, 0);

  const pick = () => {
    let roll = Math.random() * totalWeight;
    for (const item of pool) {
      roll -= item.weight;
      if (roll < 0) return item.name;
    }
    return null;
  };

    // 15s..120s, squared so shorter rests are common and long ones occasional.
  const nextDelay = () => 15000 + (Math.random() * Math.random()) * 120000;
  const schedule = () => window.setTimeout(playOnce, nextDelay());

  let classes = [];

  function playOnce() {
    const name = pick();
    if (!name) {
      schedule();
      return;
    }
    classes.forEach((c) => glow.classList.remove(c));
    classes = [name];
    glow.classList.add(name);
    glow.addEventListener(
      'animationend',
      () => {
        glow.classList.remove(name);
        classes = [];
        schedule();
      },
      { once: true },
    );
  }

  schedule();
})();

// Living headline: every so often the hero title crossfades to a playful variant
// and stays there, so the page keeps rewording itself at an unpredictable pace.
(() => {
  const heading = document.querySelector('.layout aside h1');
  if (!heading) return;
  if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;

  const original = heading.textContent.trim();
  // Only the "Experience the progress." heroes riff; leave other titles alone.
  if (original !== 'Experience the progress.') return;

  const variants = [
    'Experience the progress.',
    'Experience the progress',
    'Experience the progress…',
    'Experience the process.',
    'Experiment in progress.',
    'Improvement in progress.',
    'Improvement in process.',
    'Embrace the progress.',
    'Embrace the experiment',
    'Experiment with the process.',
    'Progress, in progress.',
    'Progress the experience',
    'Progress the process',
  ];

  let current = original;

  const pickVariant = () => {
    let next = current;
    while (next === current) {
      next = variants[Math.floor(Math.random() * variants.length)];
    }
    return next;
  };

  // 30s..170s, biased short so it rewords fairly often but never on a beat.
  const nextDelay = () => 30000 + (t = Math.random() * Math.random()) * 140000;
  const schedule = () => window.setTimeout(swap, nextDelay());

  function swap() {
    const next = pickVariant();
    heading.classList.add('headline-swap');
    heading.addEventListener(
      'transitionend',
      () => {
        current = next;
        heading.textContent = next;
        heading.classList.remove('headline-swap');
        schedule();
      },
      { once: true },
    );
  }

  schedule();
})();
