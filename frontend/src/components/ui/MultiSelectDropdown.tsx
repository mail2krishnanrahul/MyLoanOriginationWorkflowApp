import { useState, useRef, useEffect } from 'react';
import { Check, ChevronDown } from 'lucide-react';
import { cn } from '@/lib/cn';

export interface MultiSelectOption {
    label: string;
    value: string;
}

interface MultiSelectDropdownProps {
    options: MultiSelectOption[];
    selectedValues: string[];
    onChange: (values: string[]) => void;
    placeholder?: string;
    className?: string;
}

export function MultiSelectDropdown({ options, selectedValues, onChange, placeholder = 'Select options', className }: MultiSelectDropdownProps) {
    const [isOpen, setIsOpen] = useState(false);
    const containerRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        const handleClickOutside = (event: MouseEvent) => {
            if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
                setIsOpen(false);
            }
        };
        document.addEventListener('mousedown', handleClickOutside);
        return () => document.removeEventListener('mousedown', handleClickOutside);
    }, []);

    const toggleOption = (value: string) => {
        const nextValues = selectedValues.includes(value)
            ? selectedValues.filter((v) => v !== value)
            : [...selectedValues, value];
        onChange(nextValues);
    };

    const selectedCount = selectedValues.length;
    const label = selectedCount === 0 ? placeholder : `${placeholder} (${selectedCount})`;

    return (
        <div className={cn('relative inline-block text-left w-full h-full', className)} ref={containerRef}>
            <button
                type="button"
                onClick={() => setIsOpen(!isOpen)}
                className="flex h-9 w-full items-center justify-between rounded-md border border-neutral-200 bg-white px-3 py-1 text-sm shadow-sm transition-colors hover:bg-neutral-50 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 dark:border-neutral-700 dark:bg-neutral-900 dark:hover:bg-neutral-800"
                aria-haspopup="listbox"
                aria-expanded={isOpen}
            >
                <span className={cn('truncate', selectedCount === 0 && 'text-neutral-500')}>{label}</span>
                <ChevronDown className={cn('ml-2 size-4 shrink-0 text-neutral-500 transition-transform duration-200', isOpen && 'rotate-180')} />
            </button>

            {isOpen && (
                <div className="absolute z-50 mt-1 max-h-60 w-full min-w-[200px] overflow-auto rounded-md border border-neutral-200 bg-white p-1 text-base shadow-lg ring-1 ring-black/5 focus:outline-none sm:text-sm dark:border-neutral-700 dark:bg-neutral-900">
                    {options.map((option) => {
                        const isSelected = selectedValues.includes(option.value);
                        return (
                            <div
                                key={option.value}
                                onClick={() => toggleOption(option.value)}
                                className={cn(
                                    'relative flex cursor-pointer select-none items-center rounded-sm px-2 py-1.5 text-sm hover:bg-neutral-100 dark:hover:bg-neutral-800',
                                    isSelected && 'bg-brand-50 text-brand-900 dark:bg-brand-500/10 dark:text-brand-100'
                                )}
                            >
                                <div className={cn(
                                    'mr-2 flex size-4 items-center justify-center rounded-sm border',
                                    isSelected
                                        ? 'border-brand-600 bg-brand-600 dark:border-brand-500 dark:bg-brand-500'
                                        : 'border-neutral-300 dark:border-neutral-600'
                                )}>
                                    {isSelected && <Check className="size-3 text-white" strokeWidth={3} />}
                                </div>
                                <span className="truncate">{option.label}</span>
                            </div>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
