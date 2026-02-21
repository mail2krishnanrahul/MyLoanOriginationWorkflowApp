import { AnimatePresence, motion } from 'framer-motion';
import { X } from 'lucide-react';
import {
  useEffect,
  useId,
  useRef,
  type PropsWithChildren,
  type ReactNode,
  type KeyboardEvent
} from 'react';
import { createPortal } from 'react-dom';
import { Button } from '@/components/ui/Button';
import { cn } from '@/lib/cn';

interface ModalProps extends PropsWithChildren {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  footer?: ReactNode;
  size?: 'sm' | 'md' | 'lg' | 'full';
}

const sizeClasses: Record<NonNullable<ModalProps['size']>, string> = {
  sm: 'max-w-lg',
  md: 'max-w-2xl',
  lg: 'max-w-4xl',
  full: 'max-w-[96vw]'
};

export function Modal({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  size = 'md'
}: ModalProps) {
  const titleId = useId();
  const descriptionId = useId();
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) {
      return;
    }

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    panelRef.current?.focus();

    return () => {
      document.body.style.overflow = previousOverflow;
    };
  }, [open]);

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') {
      onOpenChange(false);
    }
  };

  return createPortal(
    <AnimatePresence>
      {open ? (
        <div className="fixed inset-0 z-50 flex items-end justify-center p-4 sm:items-center" onKeyDown={onKeyDown}>
          <motion.button
            type="button"
            aria-label="Close modal"
            className="absolute inset-0 bg-neutral-950/50 backdrop-blur-sm"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={() => onOpenChange(false)}
          />
          <motion.div
            ref={panelRef}
            role="dialog"
            aria-modal="true"
            aria-labelledby={titleId}
            aria-describedby={description ? descriptionId : undefined}
            tabIndex={-1}
            initial={{ opacity: 0, y: 20, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 20, scale: 0.98 }}
            transition={{ duration: 0.2 }}
            className={cn(
              'relative z-10 flex max-h-[95vh] w-full flex-col overflow-hidden rounded-2xl border border-neutral-200 bg-white shadow-2xl dark:border-neutral-700 dark:bg-neutral-900',
              sizeClasses[size],
              size === 'full' ? 'h-[92vh]' : 'h-auto'
            )}
          >
            <div className="flex items-start justify-between border-b border-neutral-200 px-6 py-4 dark:border-neutral-700">
              <div>
                <h2 id={titleId} className="text-lg font-semibold text-neutral-900 dark:text-neutral-50">
                  {title}
                </h2>
                {description ? (
                  <p id={descriptionId} className="mt-1 text-sm text-neutral-500 dark:text-neutral-300">
                    {description}
                  </p>
                ) : null}
              </div>
              <Button variant="ghost" aria-label="Close" onClick={() => onOpenChange(false)}>
                <X className="size-4" aria-hidden="true" />
              </Button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto p-6">{children}</div>
            {footer ? (
              <div className="border-t border-neutral-200 px-6 py-4 dark:border-neutral-700">{footer}</div>
            ) : null}
          </motion.div>
        </div>
      ) : null}
    </AnimatePresence>,
    document.body
  );
}
