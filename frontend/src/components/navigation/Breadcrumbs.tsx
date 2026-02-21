import { ChevronRight } from 'lucide-react';
import { Link, useLocation } from 'react-router-dom';

const labelMap: Record<string, string> = {
  cases: 'Cases',
  workbaskets: 'Workbaskets'
};

function readableSegment(segment: string) {
  if (labelMap[segment]) {
    return labelMap[segment];
  }

  if (segment.length > 16) {
    return segment.slice(0, 8).toUpperCase();
  }

  return segment.charAt(0).toUpperCase() + segment.slice(1);
}

export function Breadcrumbs() {
  const location = useLocation();
  const segments = location.pathname.split('/').filter(Boolean);

  return (
    <nav aria-label="Breadcrumb">
      <ol className="flex flex-wrap items-center gap-2 text-xs text-neutral-500 dark:text-neutral-300">
        <li>
          <Link to="/" className="font-medium hover:text-brand-600 dark:hover:text-brand-200">
            Home
          </Link>
        </li>
        {segments.map((segment, index) => {
          const href = `/${segments.slice(0, index + 1).join('/')}`;
          const isLast = index === segments.length - 1;

          return (
            <li key={href} className="inline-flex items-center gap-2">
              <ChevronRight className="size-3" aria-hidden="true" />
              {isLast ? (
                <span className="font-semibold text-neutral-700 dark:text-neutral-100">
                  {readableSegment(segment)}
                </span>
              ) : (
                <Link to={href} className="hover:text-brand-600 dark:hover:text-brand-200">
                  {readableSegment(segment)}
                </Link>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
