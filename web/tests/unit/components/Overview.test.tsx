import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Overview } from '../../../src/components/Overview';
import { REDIS } from '../../../src/data';

describe('Overview', () => {
  beforeEach(() => {
    // VersionBadge fetches on mount; keep it pending.
    global.fetch = vi.fn().mockReturnValue(new Promise(() => {}));
  });

  it('renders the hero, blurb and every overview section', () => {
    const { container } = render(<Overview />);
    expect(container.querySelector('#view-overview')).not.toBeNull();
    expect(screen.getByRole('heading', { level: 2, name: /redis/i })).toBeInTheDocument();
    expect(screen.getByText(REDIS.blurb)).toBeInTheDocument();
    // The install / quick-start / comparison / going-further / features sections.
    for (const suffix of ['install', 'quick', 'cmp', 'more', 'feat']) {
      expect(container.querySelector(`#${REDIS.id}-${suffix}`)).not.toBeNull();
    }
    expect(screen.getByRole('heading', { name: 'Features' })).toBeInTheDocument();
    expect(container.querySelectorAll('ul.feat li').length).toBe(REDIS.features.length);
  });
});
