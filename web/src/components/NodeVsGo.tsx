import { CompareCard, hi } from 'go-ui';
import type { Lib } from '../data';

export interface NodeVsGoProps {
  lib: Lib;
}

// NodeVsGo renders the side-by-side redis-cli → Go comparison columns: the
// upstream redis-cli (RESP) snippet next to its idiomatic Go port.
export function NodeVsGo({ lib }: NodeVsGoProps) {
  return (
    <>
      <div className="sec-h" id={`${lib.id}-cmp`}><span className="bar" /><h3 style={{ margin: 0 }}>redis-cli → Go</h3></div>
      <div className="compare">
        <CompareCard name="redis-cli" color="var(--node)" html={hi(lib.node_code)} />
        <CompareCard name="Go" color="var(--go)" html={hi(lib.go_code)} />
      </div>
    </>
  );
}
