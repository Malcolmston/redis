import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QuickStart } from '../../../src/components/QuickStart';
import { REDIS } from '../../../src/data';

describe('QuickStart', () => {
  it('renders the Quick start heading and highlighted Go snippet', () => {
    const { container } = render(<QuickStart lib={REDIS} />);
    expect(container.querySelector(`#${REDIS.id}-quick`)).not.toBeNull();
    expect(screen.getByRole('heading', { name: 'Quick start' })).toBeInTheDocument();
    // The snippet mentions cv.Canny.
    expect(container.textContent).toContain('cv.Canny');
  });
});
