import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Hero } from '../../../src/components/Hero';
import { REDIS } from '../../../src/data';

describe('Hero', () => {
  beforeEach(() => {
    // VersionBadge fetches on mount; keep it pending so the hero renders cleanly.
    global.fetch = vi.fn().mockReturnValue(new Promise(() => {}));
  });

  it('renders the name, package path and tagline', () => {
    render(<Hero lib={REDIS} />);
    expect(screen.getByRole('heading', { level: 2, name: /redis/i })).toBeInTheDocument();
    expect(screen.getByText(REDIS.pkg)).toBeInTheDocument();
    expect(screen.getByText(REDIS.tagline)).toBeInTheDocument();
  });

  it('renders the GitHub link opening in a new tab', () => {
    render(<Hero lib={REDIS} />);
    const github = screen.getByRole('link', { name: /GitHub/ });
    expect(github).toHaveAttribute('href', REDIS.repo);
    expect(github).toHaveAttribute('target', '_blank');
    expect(github).toHaveAttribute('rel', expect.stringContaining('noopener'));
  });
});
