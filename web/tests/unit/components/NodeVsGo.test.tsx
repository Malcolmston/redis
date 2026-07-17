import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { NodeVsGo } from '../../../src/components/NodeVsGo';
import { REDIS } from '../../../src/data';

describe('NodeVsGo', () => {
  it('renders the comparison heading and both redis-cli and Go columns', () => {
    const { container } = render(<NodeVsGo lib={REDIS} />);
    expect(container.querySelector(`#${REDIS.id}-cmp`)).not.toBeNull();
    expect(screen.getByText('redis-cli')).toBeInTheDocument();
    expect(screen.getByText('Go')).toBeInTheDocument();
    expect(container.querySelectorAll('.compare .code').length).toBe(2);
  });
});
